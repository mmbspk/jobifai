package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// Cost/replay fixtures include an explicit semantic review between cold and
// warm runs. The helper cannot turn an opaque saved value into a mapping.
func reviewFixtureMappings(t *testing.T, store *DBMappingStore, snap StepSnapshot, resolutions []FieldResolution, profiles ...*domain.ResumeProfile) error {
	t.Helper()
	profile := basicProfile()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	if err := store.RecordProposals(context.Background(), snap, StepInterpretation{Fields: resolutions}); err != nil {
		return err
	}
	items, err := ListFormMappingReviews(context.Background(), store.DB, store.UserID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Provenance != "pending" || item.BindingKey == "" {
			continue
		}
		if err := ConfirmFormMapping(context.Background(), store.DB, store.UserID, item.ID, item.BindingKey, profile); err != nil {
			return err
		}
	}
	return nil
}

func verifiedNoticeFixture() (StepSnapshot, []LLMFieldProposal) {
	return makeSnap(textField("f0", "How much time must you give your employer before leaving?")), []LLMFieldProposal{{
		Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.notice_period",
	}}
}

func TestMappingVerification_FormAcceptanceCannotTrustLLMMeaning(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "candidate"}
	snap, proposals := verifiedNoticeFixture()
	snap.EmployerKey = "employer-a"
	si := &StepInterpreter{Mapping: store, LLM: &stubLLM{proposals: proposals}}
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, interp.Paused)
	require.NoError(t, store.RecordProposals(context.Background(), snap, interp))
	writeConfirmedStep(context.Background(), store, snap, interp, StepExecResult{Outcome: StepOutcomeAdvanced, FilledRefs: []string{"f0"}}, false)
	_, hit := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), snap.EmployerKey)
	assert.False(t, hit, "successful form validation cannot verify question meaning")
	var value, provenance string
	require.NoError(t, db.QueryRow("SELECT resolved_value,provenance FROM form_field_mappings WHERE user_id=?", "candidate").Scan(&value, &provenance))
	assert.Empty(t, value, "mapping table cannot retain candidate answers")
	assert.Equal(t, "pending", provenance)
}

func TestMappingVerification_ReviewedBindingResolvesUpdatedAndClearedProfile(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "candidate"}
	snap, proposals := verifiedNoticeFixture()
	snap.EmployerKey = "employer-a"
	cold := &StepInterpreter{Mapping: store, LLM: &stubLLM{proposals: proposals}}
	interp, err := cold.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, store.RecordProposals(context.Background(), snap, interp))
	items, err := ListFormMappingReviews(context.Background(), db, "candidate")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NoError(t, ConfirmFormMapping(context.Background(), db, "candidate", items[0].ID, "application_defaults.notice_period", basicProfile()))
	require.Error(t, ConfirmFormMapping(context.Background(), db, "other-candidate", items[0].ID, "application_defaults.notice_period", basicProfile()))
	offline := &stubLLM{err: errors.New("model disabled")}
	// Fresh store/interpreter represent a worker restart. Live values are resolved
	// again on each lookup, with no answer retained in the mapping database.
	warm := &StepInterpreter{Mapping: &DBMappingStore{DB: db, UserID: "candidate"}, LLM: offline}
	changed := basicProfile()
	changed.ApplicationDefaults.NoticePeriod = "1 week"
	result, err := warm.InterpretStep(context.Background(), snap, changed)
	require.NoError(t, err)
	require.False(t, result.Paused)
	assert.Equal(t, "1 week", result.Fields[0].Value)
	assert.Zero(t, offline.calls)
	changed.ApplicationDefaults.NoticePeriod = ""
	result, err = warm.InterpretStep(context.Background(), snap, changed)
	require.NoError(t, err)
	assert.True(t, result.Paused)
	assert.Empty(t, result.Fields[0].Value)
	assert.Equal(t, PendingMissingProfile, result.Fields[0].Reason)
	assert.Zero(t, offline.calls, "a known question with a missing fact goes to the candidate, not the model")
	_, otherEmployer := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "employer-b")
	assert.False(t, otherEmployer)
	require.NoError(t, store.Invalidate(context.Background(), snap.Fields[0].FieldSignature("linkedin")))
	_, hit := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), snap.EmployerKey)
	assert.False(t, hit)
}

func TestMappingVerification_OpaqueAnswersAndInvalidBindingsCannotActivate(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "candidate"}
	snap, _ := verifiedNoticeFixture()
	for _, r := range []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, Value: "old answer", IntentType: FieldIntentLLM},
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, Value: "old answer", IntentType: FieldIntentProfile, BindingKey: "nonexistent"},
		{Ref: "f0", Source: SourceLLM, Action: ActionSkip, Value: "old answer"},
	} {
		require.NoError(t, store.Write(context.Background(), snap, []FieldResolution{r}))
	}
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM form_field_mappings").Scan(&n))
	assert.Zero(t, n)
}

func TestMappingVerification_ReviewedAnswerFillsMissingSponsorshipFact(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	profile := basicProfile()
	profile.ApplicationDefaults.RequiresSponsorship = nil
	snap := makeSnap(radioField("f0", "Require Sponsorship", "Yes", "No"))
	snap.Fields[0].Required = true
	snap.EmployerKey = "employer"
	scopeJobDependentFields(&snap, linkedInJob{ID: "123", URL: "https://www.linkedin.com/jobs/view/123", Title: "Coordinator", Location: "Sydney, Australia"})
	cold, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.True(t, cold.Paused)
	require.NoError(t, store.RecordProposals(context.Background(), snap, cold))
	items, err := ListFormMappingReviews(context.Background(), store.DB, store.UserID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	key := "mapping_" + items[0].ID
	profile.ApplicationDefaults.FormAnswers[key] = "No"
	require.NoError(t, ConfirmFormMapping(context.Background(), store.DB, store.UserID, items[0].ID, "application_defaults.form_answers."+key, profile))
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, "No", warm.Fields[0].Value)
	other := makeSnap(radioField("f0", "Require Sponsorship", "Yes", "No"))
	other.Fields[0].Required = true
	other.EmployerKey = snap.EmployerKey
	scopeJobDependentFields(&other, linkedInJob{ID: "456", URL: "https://www.linkedin.com/jobs/view/456", Title: "Coordinator", Location: "Seattle, USA"})
	elsewhere, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), other, profile)
	require.NoError(t, err)
	require.True(t, elsewhere.Paused, "a reviewed answer for this vacancy is not eligibility evidence for another country")
	require.Empty(t, elsewhere.Fields[0].Value)
	profile.ApplicationDefaults.FormAnswers[key] = ""
	cleared, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.True(t, cleared.Paused)
	require.Empty(t, cleared.Fields[0].Value)
}

func TestMappingVerification_FailedDeterministicFieldCreatesReview(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	snap := makeSnap(textField("f0", "First name"))
	snap.EmployerKey = "issuer"
	interp, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, interp.Paused)
	se := NewStepExecutor(basicBounds(), nil)
	filler := &stubFiller{fillOK: false}
	result, err := se.ExecuteStep(context.Background(), snap, interp, filler)
	require.Error(t, err)
	invalidateFailedMappings(context.Background(), store, snap, interp, result)
	items, err := ListFormMappingReviews(context.Background(), store.DB, store.UserID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "First name", items[0].Field.Label)
	assert.Equal(t, "pending", items[0].Provenance)
	_, hit := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	assert.False(t, hit)
}

func TestMappingVerification_RoleAnswerCannotCrossVacancies(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	first := makeSnap(textField("f0", "Why are you interested in this role?"), textField("f1", "How much notice must you give?"))
	first.EmployerKey = "issuer"
	scopeJobDependentFields(&first, linkedInJob{ID: "first", URL: "https://www.linkedin.com/jobs/view/first", Title: "Coordinator"})
	require.NoError(t, store.RecordProposals(context.Background(), first, StepInterpretation{Fields: []FieldResolution{{Ref: "f0", Source: SourceReview, Pending: true}}}))
	items, err := ListFormMappingReviews(context.Background(), store.DB, store.UserID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	profile := basicProfile()
	profile.ApplicationDefaults.FormAnswers = map[string]string{"mapping_" + items[0].ID: "This vacancy fits my plans"}
	require.NoError(t, ConfirmFormMapping(context.Background(), store.DB, store.UserID, items[0].ID, "application_defaults.form_answers.mapping_"+items[0].ID, profile))
	oneField := first
	oneField.Fields = first.Fields[:1]
	result, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), oneField, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	assert.Equal(t, "This vacancy fits my plans", result.Fields[0].Value)
	second := makeSnap(textField("f0", "Why are you interested in this role?"), textField("f1", "How much notice must you give?"))
	second.EmployerKey = first.EmployerKey
	scopeJobDependentFields(&second, linkedInJob{ID: "second", URL: "https://www.linkedin.com/jobs/view/second", Title: "Manager"})
	assert.NotEqual(t, first.Fields[0].FieldSignature(first.Platform), second.Fields[0].FieldSignature(second.Platform))
	assert.Equal(t, first.Fields[1].FieldSignature(first.Platform), second.Fields[1].FieldSignature(second.Platform), "candidate facts keep partial reuse")
	second.Fields = second.Fields[:1]
	result, err = (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), second, profile)
	require.NoError(t, err)
	assert.True(t, result.Paused)
	assert.Empty(t, result.Fields[0].Value)
}
