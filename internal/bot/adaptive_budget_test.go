package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveBudget_BatchesStopAtAttemptCapAndCancellation(t *testing.T) {
	delegate := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.notice_period"}}}
	interpreter := &StepInterpreter{LLM: boundFormLLM(delegate)}
	ctx := context.WithValue(context.Background(), adaptiveBudgetKey{}, &adaptiveRunBudget{maxCalls: 2})
	snap := makeSnap(textField("f0", "How soon can you leave?"))
	for i := 0; i < 2; i++ {
		result, err := interpreter.InterpretStep(ctx, snap, basicProfile())
		require.NoError(t, err)
		require.False(t, result.Paused)
	}
	result, err := interpreter.InterpretStep(ctx, snap, basicProfile())
	require.ErrorIs(t, err, ErrLLMLimitExceeded)
	assert.False(t, result.Paused, "attempt budget failures remain operational")
	assert.Equal(t, 2, delegate.calls)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = boundFormLLM(delegate).InterpretFields(canceled, basicProfile(), snap, []string{"f0"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 2, delegate.calls)
}

func TestAdaptiveMapping_InvalidationAffectsOnlyFailedFieldAndEmployer(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "scope-test"}
	snap := makeSnap(textField("f0", "How soon can you leave?"), textField("f1", "Target compensation?"))
	snap.EmployerKey = "issuer-a"
	fields := []FieldResolution{{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.notice_period"}, {Ref: "f1", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.salary_expectation"}}
	require.NoError(t, reviewFixtureMappings(t, store, snap, fields))
	other := snap
	other.EmployerKey = "issuer-b"
	require.NoError(t, reviewFixtureMappings(t, store, other, fields))
	result, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, result.Paused)
	invalidateFailedMappings(context.Background(), store, snap, result, StepExecResult{FieldsFailed: []string{"f0"}})
	_, ok := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	assert.False(t, ok)
	_, ok = store.LookupField(snap.Fields[1].FieldSignature(snap.Platform), snap.EmployerKey)
	assert.True(t, ok)
	_, ok = store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), other.EmployerKey)
	assert.True(t, ok)
}
