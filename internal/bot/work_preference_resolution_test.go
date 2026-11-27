package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func workPreferenceField() SnapField {
	return SnapField{Ref: "f0", Label: "Are you comfortable working in a hybrid setting?*", Group: "Additional questions", Type: SnapTypeRadio, Required: true, Options: []string{"Yes", "No"}}
}

type workPreferenceModel struct{ calls int }

func (m *workPreferenceModel) InterpretFields(context.Context, *domain.ResumeProfile, StepSnapshot, []string) ([]LLMFieldProposal, error) {
	m.calls++
	return nil, ErrLLMUnavailable
}

func TestWorkPreferences_GenericQuestionUsesCanonicalFactsWithoutModelOrReview(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		t.Run(platform, func(t *testing.T) {
			profile := basicProfile()
			before := profileDigest(profile)
			preferences := domain.WorkPreferences{Hybrid: true}
			snap := makeSnap(workPreferenceField())
			snap.Platform, snap.EmployerKey = platform, "Current employer"
			model := &workPreferenceModel{}
			missingCalls := 0
			interpreter := StepInterpreter{Preferences: &preferences, LLM: model, KnownMissing: func(context.Context, StepSnapshot, SnapField, *domain.ResumeProfile) (bool, error) {
				missingCalls++
				return true, nil // an obsolete missing-answer review has no authority
			}}
			result, err := interpreter.InterpretStep(t.Context(), snap, profile)
			require.NoError(t, err)
			require.False(t, result.Paused)
			require.False(t, result.LLMUsed)
			require.Zero(t, model.calls)
			require.Zero(t, missingCalls)
			require.Len(t, result.Fields, 1)
			require.Equal(t, SourcePreferences, result.Fields[0].Source)
			require.Equal(t, FieldIntentPreferences, result.Fields[0].IntentType)
			require.Equal(t, "work_preferences.hybrid", result.Fields[0].BindingKey)
			require.Equal(t, "Yes", result.Fields[0].Value)
			recorder := &answerRecorderStub{}
			b := New(Config{Profile: profile, Preferences: preferences, ApplicationAnswers: recorder})
			ids, err := b.prepareAnswerReviews(t.Context(), &lazyDocGen{b: b}, snap, result, profile)
			require.NoError(t, err)
			require.Empty(t, ids)
			require.Empty(t, recorder.proposals)
			require.Equal(t, before, profileDigest(profile), "preferences must not mutate or duplicate the resume profile")
		})
	}
}

func TestWorkPreferences_OnlyTheRequestedSelectedModeAndCurrentChoiceAreUsed(t *testing.T) {
	preferences := domain.WorkPreferences{Remote: true, Hybrid: true, Onsite: true}
	for _, tc := range []struct {
		question, binding string
		kind              SnapFieldType
	}{
		{"Are you comfortable working remotely?", "work_preferences.remote", SnapTypeText},
		{"Are you comfortable working in an on-site setting?", "work_preferences.onsite", SnapTypeSelect},
		{"Are you willing to work in a hybrid setting?", "work_preferences.hybrid", SnapTypeTextarea},
	} {
		field := workPreferenceField()
		field.Label, field.Group, field.Type = tc.question, tc.question, tc.kind
		field.Options = []string{"YES", "NO"}
		result, supported := workPreferenceResolution(field, &preferences, basicProfile())
		require.True(t, supported, tc.question)
		require.Equal(t, tc.binding, result.BindingKey)
		if tc.kind == SnapTypeSelect {
			require.Equal(t, "YES", result.Value, "use the current literal choice")
		}
	}
	preferences.Hybrid = false
	_, supported := workPreferenceResolution(workPreferenceField(), &preferences, basicProfile())
	require.False(t, supported, "other selected modes cannot substitute for hybrid")
	preferences.Hybrid = true
	field := workPreferenceField()
	field.Options = []string{"Yes", "YES", "No"}
	_, supported = workPreferenceResolution(field, &preferences, basicProfile())
	require.False(t, supported, "duplicate equivalent choices are ambiguous")
}

func TestWorkPreferences_BindingLearningRequiresTransitionAndRereadsCurrentSelection(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	profile := basicProfile()
	preferences := domain.WorkPreferences{Hybrid: true}
	snap := makeSnap(workPreferenceField())
	snap.EmployerKey = "Current employer"
	interpreter := StepInterpreter{Preferences: &preferences, Mapping: store}
	result, err := interpreter.InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	filled := StepExecResult{Outcome: StepOutcomeError, FilledRefs: []string{"f0"}}
	writeConfirmedStep(t.Context(), store, snap, result, filled, false)
	_, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.False(t, found, "a filled but non-advancing reference must not be promoted")
	filled.Outcome = StepOutcomeAdvanced
	writeConfirmedStep(t.Context(), store, snap, result, filled, false)
	cached, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.True(t, found)
	require.Equal(t, FieldIntentPreferences, cached.IntentType)
	require.Equal(t, "work_preferences.hybrid", cached.BindingKey)
	require.Equal(t, ProvenanceTrustedReference, cached.Provenance)
	var answer string
	require.NoError(t, store.DB.QueryRow("SELECT resolved_value FROM form_field_mappings WHERE user_id=? AND field_sig=?", store.UserID, snap.Fields[0].FieldSignature(snap.Platform)).Scan(&answer))
	require.Empty(t, answer, "the mapping stores semantics, not the candidate's answer")
	preferences.Hybrid = false
	changed, err := interpreter.InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	require.True(t, changed.Paused)
	require.Equal(t, ActionSkip, changed.Fields[0].Action)
	require.Empty(t, changed.Fields[0].Value)
	interpreter.Preferences = nil
	cleared, err := interpreter.InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	require.True(t, cleared.Paused)
	require.Empty(t, cleared.Fields[0].Value)
}

type workPreferenceMapping struct{}

func (workPreferenceMapping) LookupField(string, string) (FieldResolution, bool) {
	return FieldResolution{IntentType: FieldIntentPreferences, BindingKey: "work_preferences.hybrid", Action: ActionFill, Value: "Yes", Provenance: ProvenanceTrustedReference}, true
}

func TestWorkPreferences_QualifiedQuestionsAndOtherMeaningsCannotReusePreference(t *testing.T) {
	preferences := domain.WorkPreferences{Remote: true, Hybrid: true, Onsite: true}
	for _, question := range []string{
		"Have you worked in a hybrid setting before?",
		"How many years of hybrid work experience do you have?",
		"Are you comfortable working in a hybrid setting in Sydney?",
		"Are you comfortable working in a hybrid setting five days per week?",
		"Are you comfortable working in a hybrid setting and relocating?",
		"I agree to work in a hybrid setting and accept the employment conditions",
		"Are you comfortable working in a hybrid or onsite setting?",
	} {
		t.Run(question, func(t *testing.T) {
			field := workPreferenceField()
			field.Label = question
			result, err := (&StepInterpreter{Preferences: &preferences, Mapping: workPreferenceMapping{}}).InterpretStep(t.Context(), makeSnap(field), basicProfile())
			require.NoError(t, err)
			require.True(t, result.Paused)
			require.NotEqual(t, ActionFill, result.Fields[0].Action)
		})
	}
	for _, mutate := range []func(*SnapField){
		func(f *SnapField) { f.Group = "Our office is in Sydney" },
		func(f *SnapField) { f.HelpText = "Three office days are mandatory" },
		func(f *SnapField) { f.Options = []string{"Yes, five days in the office", "No"} },
		func(f *SnapField) { f.Type = SnapTypeNumber },
		func(f *SnapField) { f.Disabled = true },
	} {
		field := workPreferenceField()
		mutate(&field)
		_, supported := workPreferenceResolution(field, &preferences, basicProfile())
		require.False(t, supported)
	}
	profile := basicProfile()
	profile.ApplicationDefaults.CommuteConditions = "Only local commutes"
	_, supported := workPreferenceResolution(workPreferenceField(), &preferences, profile)
	require.False(t, supported, "a general preference cannot discard explicit commute conditions")
}

func TestWorkPreferences_ChangesAreRejectedBeforeAnyInputWithoutReviewRecorder(t *testing.T) {
	profile := basicProfile()
	preferences := domain.WorkPreferences{Hybrid: true}
	b := New(Config{Profile: profile, PreferencesLoader: func() *domain.WorkPreferences { copy := preferences; return &copy }})
	snap := makeSnap(workPreferenceField())
	result, err := (&StepInterpreter{Preferences: &preferences}).InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	lazy := &lazyDocGen{b: b}
	_, err = b.prepareAnswerReviews(t.Context(), lazy, snap, result, profile)
	require.NoError(t, err)
	preferences.Hybrid = false
	executor := NewStepExecutor(basicBounds(), noGuard())
	executor.BeforeSideEffect = func(context.Context) error { return b.ensureAnswerFactsCurrent(lazy) }
	filler := &stubFiller{fillOK: true, submitReady: true}
	executed, err := executor.ExecuteStep(t.Context(), snap, result, filler)
	require.ErrorContains(t, err, "work preferences changed")
	require.Equal(t, StepOutcomeError, executed.Outcome)
	require.Empty(t, executed.FilledRefs)
	require.Zero(t, filler.navCalls)
	require.Zero(t, filler.submitClicks)
	_, err = b.prepareAnswerReviews(t.Context(), &lazyDocGen{b: b}, snap, result, profile)
	require.ErrorContains(t, err, "current work preference no longer supports")
}

func TestWorkPreferences_CurrentReadFailureCannotUseCapturedAnswer(t *testing.T) {
	profile := basicProfile()
	preferences := domain.WorkPreferences{Hybrid: true}
	available := true
	b := New(Config{Profile: profile, PreferencesLoader: func() *domain.WorkPreferences {
		if !available {
			return nil
		}
		copy := preferences
		return &copy
	}})
	snap := makeSnap(workPreferenceField())
	result, err := (&StepInterpreter{Preferences: &preferences}).InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	lazy := &lazyDocGen{b: b}
	_, err = b.prepareAnswerReviews(t.Context(), lazy, snap, result, profile)
	require.NoError(t, err)
	available = false
	err = b.ensureAnswerFactsCurrent(lazy)
	require.ErrorContains(t, err, "work preferences changed")
	require.False(t, isCandidateDeferral(err), "a current-fact read failure is operational, not a candidate question")
}

func TestWorkPreferences_ProfileDependenciesStayCurrentWithoutReviewRecorder(t *testing.T) {
	for _, change := range []string{"commute conditions", "profile unavailable"} {
		t.Run(change, func(t *testing.T) {
			profile := basicProfile()
			current := profile
			preferences := domain.WorkPreferences{Hybrid: true}
			b := New(Config{Profile: profile, ProfileLoader: func() *domain.ResumeProfile { return current }, Preferences: preferences})
			snap := makeSnap(workPreferenceField())
			result, err := (&StepInterpreter{Preferences: &preferences}).InterpretStep(t.Context(), snap, profile)
			require.NoError(t, err)
			lazy := &lazyDocGen{b: b}
			_, err = b.prepareAnswerReviews(t.Context(), lazy, snap, result, profile)
			require.NoError(t, err)
			if change == "commute conditions" {
				updated := *profile
				updated.ApplicationDefaults.CommuteConditions = "Only local commutes"
				current = &updated
			} else {
				current = nil
			}
			executor := NewStepExecutor(basicBounds(), noGuard())
			executor.BeforeSideEffect = func(context.Context) error { return b.ensureAnswerFactsCurrent(lazy) }
			filler := &stubFiller{fillOK: true, submitReady: true}
			executed, err := executor.ExecuteStep(t.Context(), snap, result, filler)
			require.Error(t, err)
			require.False(t, isCandidateDeferral(err))
			require.Equal(t, StepOutcomeError, executed.Outcome)
			require.Empty(t, executed.FilledRefs)
			require.Zero(t, filler.navCalls)
			require.Zero(t, filler.submitClicks)
		})
	}
}

func TestWorkPreferences_ModelCannotGrantPreferenceReferenceAuthority(t *testing.T) {
	snap := makeSnap(workPreferenceField())
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentPreferences, BindingKey: "work_preferences.hybrid", Value: "Yes"}
	resolved := validateLLMProposals([]LLMFieldProposal{proposal}, []string{"f0"}, snap, basicProfile())
	require.Len(t, resolved, 1)
	require.True(t, resolved[0].Pending)
	require.NotEqual(t, ActionFill, resolved[0].Action)
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	err := store.Write(t.Context(), snap, []FieldResolution{{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentPreferences, BindingKey: "work_preferences.hybrid", Value: "Yes", Provenance: ProvenanceTrustedReference}})
	require.NoError(t, err)
	_, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.False(t, found)
}
