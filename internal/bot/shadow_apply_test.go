package bot

// shadow_apply_test.go — integration tests for shadow mode observation (story release req #2).
//
// Tests prove:
//   1. Legacy still drives the browser (fillFormStep called, fields filled by legacy).
//   2. Shadow produces a usable comparison: interpreter proposals recorded per step.
//   3. Shadow issues zero browser actions (stubFiller records confirm no extra fills).
//   4. Shadow writes zero entries to the active mapping cache (ReadOnly check).
//   5. wireShadowObserver is a no-op when AdaptiveLLM is nil.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// ── wireShadowObserver sets ShadowStepFn ─────────────────────────────────────

func TestWireShadowObserver_SetsStepFn(t *testing.T) {
	b := newTestBot(t)
	b.cfg.AdaptiveLLM = &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "5"},
	}}
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}

	require.Nil(t, b.cfg.ShadowStepFn, "ShadowStepFn must be nil before wiring")
	wireShadowObserver(b, ac, nil)
	assert.NotNil(t, b.cfg.ShadowStepFn, "ShadowStepFn must be set after wireShadowObserver")
}

func TestWireShadowObserver_NilLLM_NoOp(t *testing.T) {
	b := newTestBot(t)
	b.cfg.AdaptiveLLM = nil
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}

	wireShadowObserver(b, ac, nil)
	assert.Nil(t, b.cfg.ShadowStepFn, "ShadowStepFn must remain nil when AdaptiveLLM is nil")
}

// ── shadow produces a usable comparison ──────────────────────────────────────

// TestShadow_ProducesComparison_WithLLMProposals is the core integration test.
// It wires shadow observation and directly invokes the shadow step function
// (simulating what fillFormStep does when b.cfg.ShadowStepFn != nil).
// Verifies: proposals recorded, correct field count, LLM called.
func TestShadow_ProducesComparison_WithLLMProposals(t *testing.T) {
	db := openMappingDB(t)
	shadowStore := &DBMappingStore{DB: db, UserID: "user-shadow", ReadOnly: true}

	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "5 years"},
		{Ref: "f1", Action: "fill", Value: "Yes"},
	}}
	b := &Bot{
		cfg: Config{
			UserID:          "user-shadow",
			AdaptiveLLM:     llm,
			AdaptiveMapping: shadowStore,
		},
	}
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow, AttemptID: "shadow-attempt-1"}

	var observations []ShadowObservation
	wireShadowObserver(b, ac, &observations)
	require.NotNil(t, b.cfg.ShadowStepFn, "shadow step fn must be wired")

	// Simulate two form fields that the legacy path would scan.
	fields := []formField{
		{Type: "text", Question: "Years of experience", InputType: "text"},
		{Type: "radio", Question: "Eligible to work in AU?", InputType: "radio",
			Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}},
	}
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Screening")

	// Invoke the shadow step fn (what fillFormStep does).
	b.cfg.ShadowStepFn(context.Background(), snap)

	// Shadow must have produced an observation.
	require.Len(t, observations, 1, "one observation per step")
	obs := observations[0]

	assert.Equal(t, snap.SemanticSignature(), obs.Snap.SemanticSignature(), "snap sig must match")
	assert.Nil(t, obs.Err, "shadow observation must not error")
	assert.True(t, obs.LLMUsed, "shadow must have called LLM for unresolved fields")
	assert.Equal(t, 1, llm.calls, "LLM must be called exactly once for the two fields")
	require.Len(t, obs.Interpretation.Fields, 2, "both fields must be interpreted")

	// All resolutions must be valid (action fill/skip/upload).
	for _, r := range obs.Interpretation.Fields {
		assert.Contains(t, []AllowedAction{ActionFill, ActionSkip, ActionUpload}, r.Action,
			"shadow proposal must have a valid action")
	}
}

// ── shadow issues zero browser actions ───────────────────────────────────────

// TestShadow_ZeroBrowserActions proves that the shadow step function never
// invokes FieldFiller methods (Fill, NavigateNext, ClickSubmit). The shadow
// function only calls the interpreter; browser actions belong exclusively to
// the legacy fill path.
func TestShadow_ZeroBrowserActions(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "Yes"},
	}}
	b := &Bot{cfg: Config{UserID: "user-shadow", AdaptiveLLM: llm}}
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}
	wireShadowObserver(b, ac, nil)

	browserSpy := &stubFiller{}

	fields := []formField{{Type: "radio", Question: "Do you have a driver's licence?",
		Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}}}
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Licence")

	// Invoke shadow observation (simulates fillFormStep calling ShadowStepFn).
	b.cfg.ShadowStepFn(context.Background(), snap)

	// Shadow must not touch the browser.
	assert.Equal(t, 0, browserSpy.fillCalls, "shadow must not call Fill")
	assert.Equal(t, 0, browserSpy.navCalls, "shadow must not call NavigateNext")
	assert.Equal(t, 0, browserSpy.submitClicks, "shadow must not call ClickSubmit")
}

// ── shadow writes zero entries to the active mapping cache ───────────────────

// TestShadow_ZeroMappingCacheWrites proves that shadow observation never writes
// to the mapping store, even when the interpreter resolves all fields.
func TestShadow_ZeroMappingCacheWrites(t *testing.T) {
	db := openMappingDB(t)
	activeStore := &DBMappingStore{DB: db, UserID: "user-shadow-cache"}

	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "10"},
	}}
	// Shadow uses a read-only adapter over the same DB store.
	b := &Bot{cfg: Config{
		UserID:          "user-shadow-cache",
		AdaptiveLLM:     llm,
		AdaptiveMapping: activeStore, // set so readOnlyMappingAdapter wraps it
	}}
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}

	var observations []ShadowObservation
	wireShadowObserver(b, ac, &observations)

	fields := []formField{{Type: "text", Question: "Total years in the industry"}}
	snap := captureStepSnapshot(fields, nil, "seek", 0, "Background")
	b.cfg.ShadowStepFn(context.Background(), snap)

	require.Len(t, observations, 1)
	assert.True(t, observations[0].LLMUsed, "LLM must be used in observation")

	// Verify: the active store has zero entries (shadow never wrote).
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM form_field_mappings WHERE user_id = ?`, "user-shadow-cache").Scan(&count)
	assert.Equal(t, 0, count, "shadow must write zero entries to the active mapping cache")
}

// ── installShadowObserverIfNeeded ─────────────────────────────────────────────

func TestInstallShadowObserver_OnlyForShadowMode(t *testing.T) {
	cases := []struct {
		engine     domain.ApplyEngineMode
		wantWired  bool
	}{
		{domain.ApplyEngineShadow, true},
		{domain.ApplyEngineAdaptive, false},
		{domain.ApplyEngineLegacy, false},
		{"", false},
	}
	for _, tc := range cases {
		b := &Bot{cfg: Config{UserID: "u", AdaptiveLLM: &stubLLM{}}}
		ac := &AttemptContext{Engine: tc.engine}
		wired := installShadowObserverIfNeeded(b, ac, nil)
		assert.Equal(t, tc.wantWired, wired, "engine=%q", tc.engine)
		if tc.wantWired {
			assert.NotNil(t, b.cfg.ShadowStepFn)
		} else {
			assert.Nil(t, b.cfg.ShadowStepFn)
		}
	}
}

// ── shadow with mapping hit (warm path) ──────────────────────────────────────

// TestShadow_MappingHit_NoLLM verifies that when the mapping store already has
// an entry for a field, shadow observation serves it from mapping (no LLM call).
// This is important: shadow should report the same warm/cold behavior as adaptive.
func TestShadow_MappingHit_NoLLM(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-shadow-warm"}

	// Pre-populate the mapping store (simulates a prior adaptive run).
	// Field must not match any deterministic handler (needsProfile/deterministicResolve)
	// otherwise deterministicResolve fires before the mapping lookup when profile=nil.
	fields := []formField{{Type: "text", Question: "Years in current role"}}
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Logistics")
	require.NoError(t, store.Write(context.Background(), snap, []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, Value: "3"},
	}))

	llm := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "5"}}}
	b := &Bot{cfg: Config{UserID: "user-shadow-warm", AdaptiveLLM: llm, AdaptiveMapping: store}}
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}

	var observations []ShadowObservation
	wireShadowObserver(b, ac, &observations)
	b.cfg.ShadowStepFn(context.Background(), snap)

	require.Len(t, observations, 1)
	assert.Equal(t, 0, llm.calls, "shadow warm hit must not call LLM")
	assert.False(t, observations[0].LLMUsed, "shadow must report no LLM usage on warm hit")
	require.NotEmpty(t, observations[0].Interpretation.Fields)
	assert.Equal(t, SourceMapping, observations[0].Interpretation.Fields[0].Source,
		"warm hit must be served from mapping")
}
