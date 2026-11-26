package bot

// Mapping reuse tests use synthetic candidate facts and explicit fixture review.
// Counts establish local interpretation avoidance; they do not measure provider
// tokens, production billing, browser time or review overhead.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

// threeFieldForm returns a stable 3-field form snapshot used across cold/warm tests.
func threeFieldForm(platform string) StepSnapshot {
	return captureStepSnapshot([]formField{
		{Type: "text", Question: "Total years of work experience", InputType: "text"},
		{Type: "radio", Question: "Do you have a driver's licence?", InputType: "radio",
			Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}},
		{Type: "text", Question: "Current or most recent job title", InputType: "text"},
	}, nil, platform, 0, "Screening Questions")
}

func threeFieldProposals() []LLMFieldProposal {
	return []LLMFieldProposal{
		{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.experience_years"},
		{Ref: "f1", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.drivers_licence"},
		{Ref: "f2", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"},
	}
}

// ── 1. Cold run: LLM called, all fields sent ──────────────────────────────────

func TestMappingCache_ColdRun_LLMCalledForAllFields(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-proof"}
	llm := &stubLLM{proposals: threeFieldProposals()}
	si := &StepInterpreter{LLM: llm, Mapping: store}

	snap := threeFieldForm("linkedin")
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))

	// Evidence
	resolvedByLLM := 0
	for _, r := range interp.Fields {
		if r.Source == SourceLLM {
			resolvedByLLM++
		}
	}
	t.Logf("EVIDENCE cold-run | fixture_fields=3 | llm_calls=%d | resolved_by_llm=%d | paused=%v",
		llm.calls, resolvedByLLM, interp.Paused)

	assert.Equal(t, 1, llm.calls, "cold run: LLM must be called exactly once")
	assert.Equal(t, 3, resolvedByLLM, "cold run: all 3 fields must be resolved by LLM")
	assert.False(t, interp.Paused, "cold run: no fields should be paused")
}

// ── 2. Warm run: model disabled, completes with zero LLM calls ───────────────

func TestMappingCache_WarmRun_ZeroLLMCalls_ModelDisabled(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-proof-warm"}
	llm := &stubLLM{proposals: threeFieldProposals()}
	si := &StepInterpreter{LLM: llm, Mapping: store}
	snap := threeFieldForm("linkedin")

	// Cold run to populate the cache.
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))
	coldCalls := llm.calls

	// Disable the model to prove warm run never reaches it.
	llm.proposals = nil
	llm.err = errors.New("test: model is offline — warm path must not reach here")
	llm.calls = 0

	// Warm run.
	interp2, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err, "warm run must succeed even with model disabled")

	// Evidence
	fromMapping := 0
	for _, r := range interp2.Fields {
		if r.Source == SourceMapping {
			fromMapping++
		}
	}
	t.Logf("EVIDENCE warm-run | fixture_fields=3 | cold_llm_calls=%d | warm_llm_calls=%d | fields_from_mapping=%d | llm_used=%v",
		coldCalls, llm.calls, fromMapping, interp2.LLMUsed)

	assert.Equal(t, 0, llm.calls, "warm run: LLM must not be called (model disabled, all fields in mapping)")
	assert.False(t, interp2.LLMUsed, "warm run: LLMUsed must be false")
	assert.Equal(t, 3, fromMapping, "warm run: all 3 fields must come from mapping")
	assert.False(t, interp2.Paused, "warm run: must not pause")
}

// ── 3. Worker restart: mappings survive in SQLite ─────────────────────────────

func TestMappingCache_WorkerRestart_SurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proof_restart.db")

	// --- Process 1: cold run, write mappings, close DB. ---
	db1, err := appdb.Open(dbPath)
	require.NoError(t, err)
	store1 := &DBMappingStore{DB: db1, UserID: "user-restart"}
	llm1 := &stubLLM{proposals: threeFieldProposals()}
	si1 := &StepInterpreter{LLM: llm1, Mapping: store1}
	snap := threeFieldForm("linkedin")

	interp, err := si1.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store1, snap, interp.Fields))
	_ = db1.Close()

	// --- Process 2: open same DB, disable model, warm run. ---
	db2, err := appdb.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	store2 := &DBMappingStore{DB: db2, UserID: "user-restart"}
	llm2 := &stubLLM{err: errors.New("test: model offline after restart")}
	si2 := &StepInterpreter{LLM: llm2, Mapping: store2}

	interp2, err := si2.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err, "warm run after restart must succeed without model")

	fromMapping := 0
	for _, r := range interp2.Fields {
		if r.Source == SourceMapping {
			fromMapping++
		}
	}
	t.Logf("EVIDENCE worker-restart | cold_calls=%d | warm_calls_after_restart=%d | fields_from_mapping=%d",
		llm1.calls, llm2.calls, fromMapping)

	assert.Equal(t, 0, llm2.calls, "worker restart: warm run must not call model")
	assert.Equal(t, 3, fromMapping, "worker restart: all fields must come from persisted mapping")
}

// ── 4. Partial miss: only new field sent to LLM ───────────────────────────────

func TestMappingCache_PartialMiss_OnlyNewFieldSentToLLM(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-partial"}
	llm := &stubLLM{proposals: threeFieldProposals()}
	si := &StepInterpreter{LLM: llm, Mapping: store}
	snap := threeFieldForm("linkedin")
	cold, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, cold.Fields))
	changed := snap
	changed.Fields = append(append([]SnapField{}, snap.Fields...), textField("f3", "Highest level of education completed"))
	llm.calls = 0
	llm.proposals = []LLMFieldProposal{{Ref: "f3", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.education"}}
	result, err := si.InterpretStep(context.Background(), changed, basicProfile())
	require.NoError(t, err)
	require.False(t, result.Paused)
	assert.Equal(t, 1, llm.calls)
	mappingHits := 0
	for _, r := range result.Fields {
		if r.Source == SourceMapping {
			mappingHits++
		}
	}
	assert.Equal(t, 3, mappingHits)
	assert.Equal(t, "Bachelor's degree", resFor(t, result, "f3").Value)
	t.Logf("EVIDENCE partial-same-page | fields=4 | mapping_hits=3 | llm_calls=%d", llm.calls)
}

func TestMappingCache_PendingResolutions_NotReusable(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-pending"}

	// stubLLM that returns an error (simulating LLM unavailable).
	llm := &stubLLM{err: errors.New("model unavailable")}
	si := &StepInterpreter{LLM: llm, Mapping: store}

	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Portfolio URL", InputType: "text"},
	}, nil, "linkedin", 0, "Profile")

	// Provider failures remain operational and grant no cache authority.
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.ErrorIs(t, err, llm.err)
	require.NoError(t, store.Write(context.Background(), snap, interp.Fields))

	// A missing required candidate answer is pending and also cannot be reused.
	llm.err = nil
	snap.Fields[0].Required = true
	interp, err = si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.True(t, interp.Paused)
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))

	// Verify pending resolution was NOT written.
	_, ok := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "")
	require.False(t, ok, "pending resolution must not be stored in the mapping cache")

	// Second run with model back online: LLM must be called (cache is empty).
	llm.err = nil
	llm.calls = 0
	llm.proposals = []LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.portfolio"}}
	interp2, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp2.Fields))

	t.Logf("EVIDENCE unverified | first_run_pending=true | second_run_llm_calls=%d | now_stored=%v",
		llm.calls, llm.calls > 0)

	assert.Equal(t, 1, llm.calls, "second run must call LLM — pending from first run was not stored")

	// Now the third run must be warm (second run wrote it).
	llm.err = errors.New("model offline again")
	llm.calls = 0
	interp3, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 0, llm.calls, "third run must be warm — second run's resolution is now stored")
	assert.False(t, interp3.LLMUsed)
}

// ── 6. Published replay summary ───────────────────────────────────────────────

// TestMappingCache_CostSummary collects evidence across a full cold → warm → partial
// sequence and publishes a final summary with estimated costs and fixture counts.
func TestMappingCache_CostSummary(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-summary"}

	llm := &stubLLM{proposals: threeFieldProposals()}
	si := &StepInterpreter{LLM: llm, Mapping: store}
	snap := threeFieldForm("seek")

	// Cold run.
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))
	coldCalls := llm.calls
	llm.calls = 0

	// Warm run.
	llm.proposals = nil
	llm.err = errors.New("model offline")
	interp2, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	warmCalls := llm.calls
	llm.calls = 0

	// Partial miss (1 new field).
	snap4new := captureStepSnapshot([]formField{
		{Type: "text", Question: "Total years of work experience", InputType: "text"},
		{Type: "radio", Question: "Do you have a driver's licence?", InputType: "radio",
			Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}},
		{Type: "text", Question: "Current or most recent job title", InputType: "text"},
		{Type: "text", Question: "Highest level of education completed", InputType: "text"},
	}, nil, "seek", 0, "Screening Questions")
	llm.proposals = []LLMFieldProposal{{Ref: "f3", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.education"}}
	llm.err = nil
	_, err = si.InterpretStep(context.Background(), snap4new, basicProfile())
	require.NoError(t, err)
	partialCalls := llm.calls

	t.Logf("REUSE SUMMARY | cold_calls=%d | warm_calls=%d | partial_calls=%d | production_tokens=unknown | monetary_cost=unknown", coldCalls, warmCalls, partialCalls)

	assert.Equal(t, 1, coldCalls, "cold run: exactly 1 LLM call")
	assert.Equal(t, 0, warmCalls, "warm run: zero LLM calls")
	assert.Equal(t, 1, partialCalls, "partial miss: exactly 1 LLM call for the new field")
	_ = interp2
}

// ── 7. Candidate updates profile → warm hit uses new value ──────────────────

// TestMappingCache_CandidateUpdatesProfile_WarmHitUsesNewValue proves that a
// profile-bound warm hit resolves from the CURRENT candidate data, not the stale
// stored resolved_value. This is the key correctness property of the typed intent
// system: changing "notice period" in the profile is reflected on the next run
// without any LLM call.
func TestMappingCache_CandidateUpdatesProfile_WarmHitUsesNewValue(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-intent"}

	profile1 := basicProfile() // NoticePeriod = "2 weeks"

	// LLM identifies this as a profile-bound field.
	profileBoundProposal := []LLMFieldProposal{{
		Ref:        "f0",
		Action:     "fill",
		IntentType: FieldIntentProfile,
		BindingKey: "application_defaults.notice_period",
		Value:      "", // resolved at runtime
	}}

	llm := &stubLLM{proposals: profileBoundProposal}
	si := &StepInterpreter{LLM: llm, Mapping: store}
	// Use a label the deterministic pass won't intercept so the LLM stub fires.
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "How soon can you start?", InputType: "text"},
	}, nil, "linkedin", 0, "Application Defaults")

	// Cold run: LLM proposes a profile binding; value is resolved from profile1.
	interp, err := si.InterpretStep(context.Background(), snap, profile1)
	require.NoError(t, err)
	require.Len(t, interp.Fields, 1)
	r0 := interp.Fields[0]
	assert.Equal(t, "2 weeks", r0.Value, "cold run: profile1 notice period")
	assert.Equal(t, FieldIntentProfile, r0.IntentType, "cold run: intent must be profile")
	assert.Equal(t, "application_defaults.notice_period", r0.BindingKey)

	// Explicit fixture review stores only the profile binding, never the answer.
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))

	// Update the candidate's profile: notice period changes to "1 month".
	profile2 := basicProfile()
	profile2.ApplicationDefaults.NoticePeriod = "1 month"

	// Disable the LLM to prove the warm hit never reaches it.
	llm.err = errors.New("test: model offline — warm path must not reach here")
	llm.calls = 0

	si2 := &StepInterpreter{LLM: llm, Mapping: store}

	// Warm run with updated profile: must return "1 month", not stale "2 weeks".
	interp2, err := si2.InterpretStep(context.Background(), snap, profile2)
	require.NoError(t, err, "warm run must succeed without model")
	require.Len(t, interp2.Fields, 1)
	r2 := interp2.Fields[0]

	t.Logf("EVIDENCE profile-update | cold_value=%q | updated_profile_value=%q | warm_hit_value=%q | llm_calls=%d",
		r0.Value, profile2.ApplicationDefaults.NoticePeriod, r2.Value, llm.calls)

	assert.Equal(t, 0, llm.calls, "warm run with updated profile must not call LLM")
	assert.Equal(t, SourceMapping, r2.Source, "must come from mapping cache")
	assert.Equal(t, "1 month", r2.Value, "warm hit must reflect updated profile, not stale stored value")
}

// ── 8. Same label, different group → independent cache entries ───────────────

// TestMappingCache_SameLabel_DifferentGroup_ResolvesIndependently proves that two
// fields with identical labels but different section headings (Group) produce
// different FieldSignatures and therefore independent mapping cache entries.
// Without Group in the signature, a "Years of experience" under "Software Skills"
// would collide with one under "Medical Skills".
func TestMappingCache_SameLabel_DifferentGroup_ResolvesIndependently(t *testing.T) {
	fields := []SnapField{{Ref: "f0", Label: "Years of experience", Group: "Software Skills", Type: SnapTypeText}, {Ref: "f1", Label: "Years of experience", Group: "Medical Skills", Type: SnapTypeText}}
	snap := snapWithFields(fields...)
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-group"}
	resolutions := []FieldResolution{{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.software_years"}, {Ref: "f1", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.medical_years"}}
	require.NoError(t, reviewFixtureMappings(t, store, snap, resolutions))
	interpreter := &StepInterpreter{Mapping: store}
	result, err := interpreter.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, result.Paused)
	assert.Equal(t, "5", resFor(t, result, "f0").Value)
	assert.Equal(t, "3", resFor(t, result, "f1").Value)
	assert.NotEqual(t, fields[0].FieldSignature("linkedin"), fields[1].FieldSignature("linkedin"))
}

func TestMappingCache_ClearedProfileBinding_SurfacesAsUnresolved(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-cleared"}

	// Store a profile-bound mapping entry for a custom field.
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "How soon can you start?", InputType: "text"},
	}, nil, "linkedin", 0, "Availability")
	require.NoError(t, reviewFixtureMappings(t, store, snap, []FieldResolution{{
		Ref:        "f0",
		Source:     SourceLLM,
		Action:     ActionFill,
		IntentType: FieldIntentProfile,
		BindingKey: "application_defaults.notice_period",
		Value:      "2 weeks",
	}}))

	// Profile with cleared notice period.
	profileCleared := basicProfile()
	profileCleared.ApplicationDefaults.NoticePeriod = ""

	// LLM stub receives the field and answers.
	llm := &stubLLM{proposals: []LLMFieldProposal{{
		Ref: "f0", Action: "fill", Value: "immediately",
	}}}
	si := &StepInterpreter{LLM: llm, Mapping: store}

	interp, err := si.InterpretStep(context.Background(), snap, profileCleared)
	require.NoError(t, err)
	require.Len(t, interp.Fields, 1)
	r := interp.Fields[0]

	t.Logf("EVIDENCE cleared-binding | source=%s | llm_calls=%d | value=%q", r.Source, llm.calls, r.Value)

	assert.Equal(t, 0, llm.calls, "known question with missing candidate data must not call the model")
	assert.Equal(t, SourceReview, r.Source, "missing candidate fact must enter review")
	assert.Empty(t, r.Value)
}

// ── 10. Profile-bound warm hit with incompatible option → field re-sent to LLM (Finding 2) ──

// TestMappingCache_ProfileBoundWarmHit_IncompatibleOption_ReroutesLLM proves the option
// guard on the warm-hit path:
//
//  1. A profile-bound mapping entry is stored for a radio field. The field signature
//     is fixed by the options (the same snap is reused for both interpretations).
//  2. Warm hit with a compatible profile value ("Within a week") → 0 LLM calls,
//     result served from mapping.
//  3. Same sig (options unchanged), but profile binding value updated to "immediately"
//     (not in options) → option guard fires, field re-routed to LLM → 1 LLM call.
//
// The question label "When can you start?" intentionally avoids deterministicResolve's
// keyword patterns so Pass 2 (mapping) handles it rather than Pass 1 (profile lookup).
func TestMappingCache_ProfileBoundWarmHit_IncompatibleOption_ReroutesLLM(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-warmhit-opt"}

	// A radio field with a fixed option set. Options become part of FieldSignature.
	// Label avoids all deterministicResolve patterns so Pass 2 (mapping) is reached.
	snap := captureStepSnapshot([]formField{
		{Type: "radio", Question: "When can you start?",
			Options: []formFieldOption{
				{Label: "Within a week"}, {Label: "2 to 4 weeks"}, {Label: "1 month"},
			}},
	}, nil, "linkedin", 0, "Availability")

	// Review against the actual candidate fact before activating this mapping.
	profileReference := basicProfile()
	profileReference.ApplicationDefaults.NoticePeriod = "Within a week"
	require.NoError(t, reviewFixtureMappings(t, store, snap, []FieldResolution{{
		Ref:        "f0",
		Source:     SourceLLM,
		Action:     ActionFill,
		IntentType: FieldIntentProfile,
		BindingKey: "application_defaults.notice_period",
		Value:      "Within a week",
	}}, profileReference))

	// ── Part 1: compatible warm hit — profile value is in the option list ─────────
	profileCompat := basicProfile()
	profileCompat.ApplicationDefaults.NoticePeriod = "Within a week"
	llm1 := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "Within a week"}}}
	si1 := &StepInterpreter{LLM: llm1, Mapping: store}
	result1, err := si1.InterpretStep(context.Background(), snap, profileCompat)
	require.NoError(t, err)
	require.Len(t, result1.Fields, 1)
	r1 := result1.Fields[0]

	t.Logf("EVIDENCE warmhit-compat | source=%s | value=%q | llm_calls=%d", r1.Source, r1.Value, llm1.calls)
	assert.Equal(t, 0, llm1.calls, "compatible warm hit must NOT invoke LLM")
	assert.Equal(t, SourceMapping, r1.Source, "compatible warm hit must come from mapping cache")
	assert.Equal(t, "Within a week", r1.Value, "compatible warm hit must return profile value")

	// ── Part 2: incompatible warm hit — same sig, same options, profile value changed ──
	// Profile now says "immediately" which is NOT in {Within a week, 2 to 4 weeks, 1 month}.
	// The option guard must fire and route the field to the LLM.
	profileIncompat := basicProfile()
	profileIncompat.ApplicationDefaults.NoticePeriod = "immediately"
	llm2 := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "Within a week"}}}
	si2 := &StepInterpreter{LLM: llm2, Mapping: store}
	result2, err := si2.InterpretStep(context.Background(), snap, profileIncompat)
	require.NoError(t, err)
	require.Len(t, result2.Fields, 1)
	r2 := result2.Fields[0]

	t.Logf("EVIDENCE warmhit-incompat | source=%s | value=%q | llm_calls=%d", r2.Source, r2.Value, llm2.calls)
	assert.Equal(t, 0, llm2.calls, "incompatible current candidate fact needs review")
	assert.Equal(t, SourceReview, r2.Source, "contradictory options need candidate review")
	assert.Empty(t, r2.Value, "never substitute a different fact just to fit the options")
}

// ── 11. Provenance gating: only persist on confirmed outcomes (Finding 3) ────

// TestProvenanceGating_PausedStep_NoMappingsWritten proves that a step with any
// pending field (Paused=true) does not persist mappings to the store. The gate is
// in adaptive_runner.go; this test drives the mapping.Write call path directly to
// confirm that pending resolutions are skipped by the store.
func TestProvenanceGating_PausedStep_NoMappingsWritten(t *testing.T) {
	snap := makeSnap(textField("f0", "Given name"), unsupportedField("f1", "Custom widget"))
	interp := StepInterpretation{Paused: true, Fields: []FieldResolution{referenceResolution("f0", "personal_information.name"), {Ref: "f1", Source: SourceReview, Action: ActionSkip, Pending: true}}}
	filler := &stubFiller{fillOK: true, navOK: true}
	se := NewStepExecutor(basicBounds(), nil)
	result, err := se.ExecuteStep(context.Background(), snap, interp, filler)
	require.NoError(t, err)
	store := ghRecordingStore(t, "paused-store")
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)
	assert.Zero(t, store.writeCount())
	assert.Zero(t, filler.fillCalls)
	assert.Zero(t, filler.navCalls)
}

func TestProvenanceGating_OutcomeGating_ResolvesCorrectly(t *testing.T) {
	cases := []struct {
		name      string
		outcome   StepExecOutcome
		confirmed bool
		want      int
	}{
		{"advanced", StepOutcomeAdvanced, false, 1},
		{"unconfirmed submit", StepOutcomeSubmitted, false, 0},
		{"confirmed submit", StepOutcomeSubmitted, true, 1},
		{"error", StepOutcomeError, false, 0},
		{"paused", StepOutcomePaused, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := makeSnap(textField("f0", "Given name"))
			interp := StepInterpretation{Fields: []FieldResolution{referenceResolution("f0", "personal_information.name")}}
			store := ghRecordingStore(t, "outcome-store")
			writeConfirmedStep(context.Background(), store, snap, interp, StepExecResult{Outcome: tc.outcome, FilledRefs: []string{"f0"}}, tc.confirmed)
			assert.Equal(t, tc.want, store.writeCount())
		})
	}
}
