package bot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func updatedSalaryRange() *domain.ApplicationSalaryRange {
	minimum, maximum, included := 120000.0, 140000.0, false
	return &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &included, BenefitsDetails: "superannuation"}
}

func TestCurrentProfileFacts_ColdSalaryUsesStructuredFacts(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.SalaryExpectation = "AUD 80,000 per year"
	profile.ApplicationDefaults.SalaryRange = updatedSalaryRange()
	snap := makeSnap(SnapField{Ref: "f0", Label: "Expected salary", Type: SnapTypeTextarea, Required: true})
	server := newGroundedCountingServer(t, func(body []byte) string {
		require.Contains(t, string(body), "salary_range")
		require.Contains(t, string(body), "authoritative over legacy")
		return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"application_defaults.salary_expectation","value":"AUD 80,000 per year"}]`
	})
	result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, "AUD 120000–140000 per year base compensation excluding superannuation", result.Fields[0].Value)
	require.Equal(t, int32(2), server.requests.Load())
	require.Equal(t, ProvenanceVerifiedByModel, result.Fields[0].Provenance)
}

func TestCurrentProfileFacts_WarmSalaryRefreshesAfterStructuredUpdate(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.SalaryExpectation = "AUD 80,000 per year"
	snap := makeSnap(SnapField{Ref: "f0", Label: "What salary would you expect?", Type: SnapTypeTextarea, Required: true})
	snap.EmployerKey = "salary-issuer"
	server := newGroundedCountingServer(t, func([]byte) string {
		return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"application_defaults.salary_expectation"}]`
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	si := &StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}
	cold, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)
	require.NoError(t, store.Write(context.Background(), snap, cold.Fields))
	profile.ApplicationDefaults.SalaryRange = updatedSalaryRange()
	changed, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, changed.Paused)
	require.Equal(t, currentSalaryExpectation(profile), changed.Fields[0].Value)
	require.NotEqual(t, cold.Fields[0].Value, changed.Fields[0].Value)
	require.Equal(t, int32(4), server.requests.Load(), "the changed salary facts require a fresh proposal and independent verification")
	require.NoError(t, store.Write(context.Background(), snap, changed.Fields))
	warm, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, changed.Fields[0].Value, warm.Fields[0].Value)
	require.Equal(t, int32(4), server.requests.Load())
	// Older records could omit the profile digest. They must not retain the
	// previous value-independent salary policy after the upgrade.
	cached, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.True(t, found)
	cached.cached.ProfileDigest, cached.cached.Evidence = "", nil
	require.False(t, reusableGroundedCache(cached.cached, profile, snap, snap.Fields[0], cached))
}

func TestCurrentProfileFacts_TrustedSalaryBindingRereadsStructuredRange(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.SalaryExpectation = "AUD 80,000 per year"
	profile.ApplicationDefaults.SalaryRange = updatedSalaryRange()
	snap := makeSnap(SnapField{Ref: "f0", Label: "What salary would you expect?", Type: SnapTypeTextarea, Required: true})
	mapping := &stubMapping{fieldSig: snap.Fields[0].FieldSignature(snap.Platform), res: FieldResolution{Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.salary_expectation", Provenance: ProvenanceTrustedReference}}
	result, err := (&StepInterpreter{Mapping: mapping}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, currentSalaryExpectation(profile), result.Fields[0].Value)
}

func TestCurrentProfileFacts_CountrySponsorshipOverridesGeneralDefault(t *testing.T) {
	for _, tc := range []struct {
		name, label, location string
		want                  string
	}{
		{"question country", "Do you require sponsorship in Australia?", "", "Yes"},
		{"vacancy country", "Do you require sponsorship?", "Sydney, Australia", "Yes"},
		{"different vacancy", "Do you require sponsorship?", "London, United Kingdom", "No"},
		{"unknown jurisdiction", "Do you require sponsorship?", "", ""},
		{"conflicting jurisdictions", "Do you require sponsorship in Australia?", "London, United Kingdom", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := basicProfile()
			yes := true
			profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", RequiresSponsorship: &yes}}
			field := SnapField{Ref: "f0", Label: tc.label, JobLocation: tc.location, Type: SnapTypeRadio, Options: []string{"Yes", "No"}, Required: true}
			server := newGroundedCountingServer(t, func([]byte) string {
				return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"application_defaults.requires_sponsorship"}]`
			})
			result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), makeSnap(field), profile)
			require.NoError(t, err)
			require.Equal(t, tc.want == "", result.Paused)
			require.Equal(t, tc.want, result.Fields[0].Value)
		})
	}
}

func TestCurrentProfileFacts_WarmSponsorshipRefreshesAfterCountryUpdate(t *testing.T) {
	profile := basicProfile()
	snap := makeSnap(SnapField{Ref: "f0", Label: "Will sponsorship be necessary for employment?", JobLocation: "Australia", Type: SnapTypeRadio, Options: []string{"Yes", "No"}, Required: true})
	snap.EmployerKey = "sponsorship-issuer"
	server := newGroundedCountingServer(t, func([]byte) string {
		return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"application_defaults.requires_sponsorship","value":"No"}]`
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	si := &StepInterpreter{Mapping: store, LLM: NewLLMFormInterpreter(server.newClient(t))}
	cold, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)
	require.Equal(t, "No", cold.Fields[0].Value)
	require.NoError(t, store.Write(context.Background(), snap, cold.Fields))
	yes := true
	profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", RequiresSponsorship: &yes}}
	changed, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, changed.Paused)
	require.Equal(t, "Yes", changed.Fields[0].Value)
	require.Equal(t, int32(4), server.requests.Load())
	require.NoError(t, store.Write(context.Background(), snap, changed.Fields))
	warm, err := si.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, "Yes", warm.Fields[0].Value)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, int32(4), server.requests.Load())
	// Trusted mappings use the same jurisdiction-aware binding accessor.
	trusted := &stubMapping{fieldSig: snap.Fields[0].FieldSignature(snap.Platform), res: FieldResolution{Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.requires_sponsorship", Provenance: ProvenanceTrustedReference}}
	result, err := (&StepInterpreter{Mapping: trusted}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, "Yes", result.Fields[0].Value)
}

func TestCurrentProfileFacts_CachedWorkRightsExpireAtSiblingVisaDate(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", Status: "Holder of a current unrestricted work visa", VisaExpiryDate: "2030-12-10"}}
	snap := makeSnap(SnapField{Ref: "f0", Label: "Are you currently authorized to work in Australia?", Type: SnapTypeRadio, Options: []string{"Yes", "No"}, Required: true})
	snap.EmployerKey = "visa-issuer"
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "Yes", Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/0/status", Quote: "Holder of a current unrestricted work visa"}}}
	raw, err := json.Marshal([]LLMFieldProposal{proposal})
	require.NoError(t, err)
	server := newGroundedCountingServer(t, func([]byte) string { return string(raw) })
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	cold, err := (&StepInterpreter{Mapping: store, LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)
	require.NoError(t, store.Write(context.Background(), snap, cold.Fields))
	cached, hit := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.True(t, hit)
	expiry, err := time.Parse("2006-01-02", profile.ApplicationDefaults.WorkRights[0].VisaExpiryDate)
	require.NoError(t, err)
	require.True(t, reusableGroundedCacheAt(cached.cached, profile, snap, snap.Fields[0], cached, expiry.Add(-time.Nanosecond)))
	require.False(t, reusableGroundedCacheAt(cached.cached, profile, snap, snap.Fields[0], cached, expiry))
	require.False(t, reusableGroundedCacheAt(cached.cached, profile, snap, snap.Fields[0], cached, expiry.Add(24*time.Hour)))
	// "No restrictions" is affirmative authority and must not be exempted
	// merely because the prose begins with No.
	require.False(t, currentWorkRightsTimeValid(profile, snap.Fields[0], "No restrictions apply to my right to work", proposal.Evidence, expiry))
	require.True(t, currentWorkRightsTimeValid(profile, snap.Fields[0], "No", proposal.Evidence, expiry))
}

func TestCurrentProfileFacts_ExpiredWorkRightsCannotReceiveFreshProof(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", Status: "Holder of a work visa", VisaExpiryDate: "2000-01-01"}}
	snap := makeSnap(SnapField{Ref: "f0", Label: "Are you currently authorized to work in Australia?", Type: SnapTypeRadio, Options: []string{"Yes", "No"}, Required: true})
	server := newGroundedCountingServer(t, func([]byte) string {
		return `[{"ref":"f0","action":"fill","intent_type":"llm","value":"Yes","evidence":[{"path":"/application_defaults/work_rights/0/status","quote":"Holder of a work visa"}]}]`
	})
	result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.True(t, result.Paused)
	require.Empty(t, result.Fields[0].Value)
	require.Equal(t, int32(1), server.requests.Load(), "an expired affirmative answer stops before independent approval")
	profile.ApplicationDefaults.FormAnswers["work_rights_au"] = "Yes"
	trusted := &stubMapping{fieldSig: snap.Fields[0].FieldSignature(snap.Platform), res: FieldResolution{Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.work_rights_au", Provenance: ProvenanceTrustedReference}}
	warm, err := (&StepInterpreter{Mapping: trusted}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.True(t, warm.Paused, "trusted explicit answers must also refresh after the applicable visa expires")
	require.Empty(t, warm.Fields[0].Value)
}

func TestCurrentProfileFacts_ProfileChoiceIgnoresUnusedModelAlias(t *testing.T) {
	for _, alias := range []string{"Immediately", "1-2 weeks", "unrelated model value"} {
		t.Run(alias, func(t *testing.T) {
			profile := basicProfile()
			profile.ApplicationDefaults.NoticePeriod = "Immediately Available"
			snap := makeSnap(SnapField{Ref: "f0", Label: "What is your notice period?", Type: SnapTypeRadio, Options: []string{"Immediately Available", "1-2 weeks"}, Required: true})
			proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.notice_period", Value: alias}
			raw, err := json.Marshal([]LLMFieldProposal{proposal})
			require.NoError(t, err)
			server := newGroundedCountingServer(t, func([]byte) string { return string(raw) })
			result, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.False(t, result.Paused)
			require.Equal(t, "Immediately Available", result.Fields[0].Value)
			require.Equal(t, ProvenanceVerifiedByModel, result.Fields[0].Provenance)
			require.Equal(t, int32(2), server.requests.Load())
		})
	}
}

func TestCurrentProfileFacts_TimeAndStructuredAuthorityReachBothPrompts(t *testing.T) {
	for _, prompt := range []string{buildBatchFormPrompt([]byte(`{}`), []byte(`{}`)), buildGroundedVerificationPrompt([]byte(`{}`), []byte(`{}`))} {
		require.Contains(t, prompt, "Current date (UTC): "+time.Now().UTC().Format("2006-01-02"))
		require.Contains(t, prompt, "Structured salary_range is authoritative")
		require.Contains(t, prompt, "Country-specific sponsorship answers override")
		require.Contains(t, prompt, "expired visa")
	}
}

func TestCurrentProfileFacts_NormalizationPreservesCustomAnswerKeys(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.FormAnswers["NoticePeriod"] = "Immediately Available"
	field := SnapField{Label: "Notice period", Type: SnapTypeText}
	value, resolved := resolveProfileBindingForField("application_defaults.form_answers.NoticePeriod", profile, field)
	require.True(t, resolved)
	require.Equal(t, "Immediately Available", value)
	require.Equal(t, []ProfileEvidence{{Path: "/application_defaults/form_answers/NoticePeriod", Quote: "Immediately Available"}}, bindingEvidenceForField(profile, "application_defaults.form_answers.NoticePeriod", field))
	require.False(t, directBindingIndependentOfValue(field, " APPLICATION_DEFAULTS.SALARY_EXPECTATION "))
	require.False(t, directBindingIndependentOfValue(field, " APPLICATION_DEFAULTS.REQUIRES_SPONSORSHIP "))
	profile.ApplicationDefaults.SalaryExpectation = "AUD 80,000 per year"
	profile.ApplicationDefaults.SalaryRange = &domain.ApplicationSalaryRange{Currency: "AUD", PayPeriod: "year", Basis: "base"}
	require.Empty(t, currentSalaryExpectation(profile), "a partially cleared range cannot revive a stale legacy salary or treat its units as an expectation")
}
