package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func unfamiliarPhoneCountrySnapshot() StepSnapshot {
	var options []formFieldOption
	for i := 0; i < 26; i++ {
		options = append(options, formFieldOption{Value: fmt.Sprintf("choice-%d", i), Label: fmt.Sprintf("Other choice %d", i)})
	}
	options = append(options, formFieldOption{Value: "country:Australia", Label: "Australia (+61)"}, formFieldOption{Value: "country:Canada", Label: "Canada (+1)"}, formFieldOption{Value: "country:USA", Label: "United States (+1)"})
	snap := captureStepSnapshot([]formField{{Type: "select", Question: "Phone country code", Required: true, Options: options}, {Type: "text", Question: "Phone number", Required: true, InputType: "tel"}}, nil, "linkedin", 0, "Contact information")
	snap.EmployerKey = "verified-issuer"
	return snap
}

func phoneFallbackProposal(profile *domain.ResumeProfile, value string) string {
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: value, Evidence: []ProfileEvidence{{Path: "/personal_information/phone", Quote: profile.PersonalInformation.Phone}, {Path: "/personal_information/country", Quote: profile.PersonalInformation.Country}}}
	raw, _ := json.Marshal([]LLMFieldProposal{proposal})
	return string(raw)
}

func TestPhoneCountryFallback_VerifiedFillLearnsAndWarmReplayNeedsNoModel(t *testing.T) {
	profile := basicProfile()
	profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = "0412 345 678", "Australia", ""
	snap := unfamiliarPhoneCountrySnapshot()
	require.True(t, snap.Fields[0].Truncated)
	choice := "Australia (+61)"
	server := newGroundedCountingServer(t, func(body []byte) string {
		require.Contains(t, string(body), "Australia (+61)")
		return phoneFallbackProposal(profile, choice)
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	cold, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused, "%+v", cold.Fields)
	require.Equal(t, int32(2), server.requests.Load(), "proposal and independent verification are separate batches")
	require.Equal(t, ProvenanceVerifiedByModel, resFor(t, cold, "f0").Provenance)
	require.Equal(t, "412345678", resFor(t, cold, "f1").Value)
	writeVerifiedFilledStep(context.Background(), store, snap, cold, StepExecResult{Outcome: StepOutcomeError, FilledRefs: []string{"f0"}})
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.False(t, warm.LLMUsed)
	require.Equal(t, SourceMapping, resFor(t, warm, "f0").Source)
	require.Equal(t, "Australia (+61)", resFor(t, warm, "f0").Value)
	require.Equal(t, int32(2), server.requests.Load())
	profile.PersonalInformation.Phone = "+1 416 555 2671"
	changed, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.True(t, changed.Paused, "a changed phone cannot reuse the old derived code")
	require.Empty(t, resFor(t, changed, "f0").Value)
	choice = "Canada (+1)"
	relearned, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, relearned.Paused, "expired long-list mappings must re-expand current choices before relearning: %+v", relearned.Fields)
	require.Equal(t, choice, resFor(t, relearned, "f0").Value)
	require.Equal(t, int32(4), server.requests.Load())
	writeVerifiedFilledStep(context.Background(), store, snap, relearned, StepExecResult{Outcome: StepOutcomeError, FilledRefs: []string{"f0"}})
	newWarm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, newWarm.Paused)
	require.Equal(t, choice, resFor(t, newWarm, "f0").Value)
	require.Equal(t, SourceMapping, resFor(t, newWarm, "f0").Source)
}

func TestPhoneCountryFallback_AmbiguityAndRecognizableConflictsDoNotCallModel(t *testing.T) {
	for _, name := range []string{"duplicate country", "misleading code", "conflicting ISO region", "missing number", "invalid number", "conflicting prefix"} {
		t.Run(name, func(t *testing.T) {
			profile := basicProfile()
			profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = "0412 345 678", "Australia", ""
			snap := unfamiliarPhoneCountrySnapshot()
			switch name {
			case "duplicate country":
				snap.Fields[0].BrowserField.Options = append(snap.Fields[0].BrowserField.Options, formFieldOption{Value: "other-Australia", Label: "Australia (+61)"})
			case "misleading code":
				snap.Fields[0].BrowserField.Options[26].Label = "Australia (+1)"
			case "conflicting ISO region":
				snap.Fields[0].BrowserField.Options[26].Value = "us"
			case "missing number":
				profile.PersonalInformation.Phone = ""
			case "invalid number":
				profile.PersonalInformation.Phone = "123"
			case "conflicting prefix":
				profile.PersonalInformation.PhonePrefix = "+1"
			}
			snap.Fields = snap.Fields[:1]
			delegate := &stubLLM{}
			result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.True(t, result.Paused)
			require.Empty(t, resFor(t, result, "f0").Value)
			require.Zero(t, delegate.calls)
		})
	}
}

func TestPhoneCountryFallback_SharedCodeUsesPhoneRegionAndRejectsWrongProposal(t *testing.T) {
	for _, tc := range []struct{ phone, country, choice string }{
		{"+1 415 555 2671", "Canada", "United States (+1)"},
		{"+1 416 555 2671", "United States", "Canada (+1)"},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			profile := basicProfile()
			profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = tc.phone, tc.country, ""
			snap := unfamiliarPhoneCountrySnapshot()
			server := newGroundedCountingServer(t, func([]byte) string { return phoneFallbackProposal(profile, tc.choice) })
			result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.False(t, result.Paused, "%+v", result.Fields)
			require.Equal(t, tc.choice, resFor(t, result, "f0").Value)
			wrong := "Canada (+1)"
			if tc.choice == wrong {
				wrong = "United States (+1)"
			}
			wrongServer := newGroundedCountingServer(t, func([]byte) string { return phoneFallbackProposal(profile, wrong) })
			wrongResult, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(wrongServer.newClient(t))}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.True(t, wrongResult.Paused)
			require.Empty(t, resFor(t, wrongResult, "f0").Value)
			require.Equal(t, int32(1), wrongServer.requests.Load(), "a contradictory proposal is rejected before independent verification")
		})
	}
}

func TestPhoneCountryFallback_IndependentVerifierMustApprove(t *testing.T) {
	profile := basicProfile()
	profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = "0412 345 678", "Australia", ""
	server := newCountingServer(t, func(body []byte) string {
		if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
			return fixtureGroundedVerdicts(body, false)
		}
		return phoneFallbackProposal(profile, "Australia (+61)")
	})
	result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), unfamiliarPhoneCountrySnapshot(), profile)
	require.NoError(t, err)
	require.True(t, result.Paused)
	require.Empty(t, resFor(t, result, "f0").Value)
	require.Equal(t, int32(2), server.requests.Load())
}
