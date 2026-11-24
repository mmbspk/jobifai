package bot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestGroundedProfileIntegration_StructuredSalaryReachesModelAndCache(t *testing.T) {
	profile := basicProfile()
	profile.Summary, profile.PromptInstructions = "", ""
	profile.ApplicationDefaults.FormAnswers = nil
	profile.ApplicationDefaults.SalaryExpectation = ""
	minimum, maximum := 110000.0, 130000.0
	included := false
	profile.ApplicationDefaults.SalaryRange = &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &included, BenefitsDetails: "superannuation"}
	snap := makeSnap(SnapField{Ref: "f0", Label: "Salary expectation", Type: SnapTypeTextarea, Required: true})
	snap.EmployerKey = "issuer"
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "AUD 110,000-130,000 per year in base pay, excluding superannuation", Evidence: []ProfileEvidence{
		{Path: "/application_defaults/salary_range/minimum", Quote: "110000"},
		{Path: "/application_defaults/salary_range/maximum", Quote: "130000"},
		{Path: "/application_defaults/salary_range/currency", Quote: "AUD"},
		{Path: "/application_defaults/salary_range/pay_period", Quote: "year"},
		{Path: "/application_defaults/salary_range/basis", Quote: "base"},
		{Path: "/application_defaults/salary_range/includes_benefits", Quote: "false"},
		{Path: "/application_defaults/salary_range/benefits_details", Quote: "superannuation"},
	}}
	raw, _ := json.Marshal([]LLMFieldProposal{proposal})
	server := newGroundedCountingServer(t, func([]byte) string { return string(raw) })
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	si := &StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}
	cold, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)
	require.Equal(t, proposal.Value, resFor(t, cold, "f0").Value)
	require.Equal(t, int32(2), server.requests.Load())
	require.NoError(t, store.Write(context.Background(), snap, cold.Fields))
	warm, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.False(t, warm.LLMUsed)
	require.Equal(t, int32(2), server.requests.Load())
}

func TestGroundedProfileIntegration_AvailabilityIsSeparateFromNotice(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.NoticePeriod = "4 weeks"
	profile.ApplicationDefaults.Availability = "Available from 15 December after a planned commitment"
	profile.ApplicationDefaults.EarliestStartDate = "2026-12-15"
	snap := makeSnap(textField("f0", "Availability"), textField("f1", "Notice period"), textField("f2", "Earliest start date"))
	result, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, profile.ApplicationDefaults.Availability, resFor(t, result, "f0").Value)
	require.Equal(t, "4 weeks", resFor(t, result, "f1").Value)
	require.Equal(t, "2026-12-15", resFor(t, result, "f2").Value)
}

func TestGroundedProfileIntegration_CountryFactsCannotBorrowVerifierApproval(t *testing.T) {
	for _, source := range []string{"work_rights", "custom_answers"} {
		for _, tc := range []struct {
			name, label, location string
			allowed               bool
		}{
			{"matching jurisdiction", "Describe your right to work in Australia", "", true},
			{"other jurisdiction", "Describe your right to work in the United Kingdom", "", false},
			{"unknown jurisdiction", "Describe your right to work", "", false},
			{"job supplies jurisdiction", "Describe your right to work", "Sydney, Australia", true},
			{"contradictory jurisdiction", "Describe your right to work in Australia", "London, United Kingdom", false},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				profile := basicProfile()
				profile.PromptInstructions, profile.Summary = "", ""
				var evidence []ProfileEvidence
				if source == "work_rights" {
					profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", Status: "Australian permanent resident"}}
					evidence = []ProfileEvidence{{Path: "/application_defaults/work_rights/0/status", Quote: "Australian permanent resident"}}
				} else {
					profile.ApplicationDefaults.CustomAnswers = []domain.ApplicationAnswer{{Question: "Describe your right to work", Answer: "Australian permanent resident", Country: "Australia"}}
					evidence = []ProfileEvidence{{Path: "/application_defaults/custom_answers/0/answer", Quote: "Australian permanent resident"}}
				}
				proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "I am an Australian permanent resident.", Evidence: evidence}
				raw, _ := json.Marshal([]LLMFieldProposal{proposal})
				server := newGroundedCountingServer(t, func([]byte) string { return string(raw) })
				snap := makeSnap(SnapField{Ref: "f0", Label: tc.label, Type: SnapTypeTextarea, Required: true, JobLocation: tc.location, Locale: "en-AU"})
				result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
				require.NoError(t, err)
				require.Equal(t, !tc.allowed, result.Paused)
				if tc.allowed {
					require.Equal(t, int32(2), server.requests.Load())
				} else {
					require.Equal(t, int32(1), server.requests.Load(), "wrong or unknown country must stop before verifier approval")
					require.Empty(t, result.Fields[0].Value)
				}
			})
		}
	}
}

func TestGroundedProfileIntegration_VerifiedModelFillFailureInvalidatesOnlyItsIssuer(t *testing.T) {
	profile := groundedFixtureProfile()
	snap := groundedFixtureSnapshot()
	snap.Fields = snap.Fields[:1]
	server := newGroundedCountingServer(t, func(body []byte) string { return groundedFixtureProposals(extractRefsFromPromptBody(body), false) })
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	si := &StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}
	interp, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, interp.Paused)
	require.NoError(t, store.RecordProposals(context.Background(), snap, interp))
	require.NoError(t, store.Write(context.Background(), snap, interp.Fields))
	_, hit := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.True(t, hit)
	other := snap
	other.EmployerKey = "another issuer"
	otherInterp, err := si.InterpretStep(context.Background(), other, profile)
	require.NoError(t, err)
	require.NoError(t, store.RecordProposals(context.Background(), other, otherInterp))
	require.NoError(t, store.Write(context.Background(), other, otherInterp.Fields))
	invalidateFailedMappings(context.Background(), store, snap, interp, StepExecResult{FieldsFailed: []string{"f0"}})
	var provenance string
	require.NoError(t, store.DB.QueryRow("SELECT provenance FROM form_field_mappings WHERE user_id=? AND employer_key=?", store.UserID, snap.EmployerKey).Scan(&provenance))
	require.Equal(t, "invalidated", provenance)
	_, hit = store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.False(t, hit)
	_, hit = store.LookupField(other.Fields[0].FieldSignature(other.Platform), other.EmployerKey)
	require.True(t, hit)
}
