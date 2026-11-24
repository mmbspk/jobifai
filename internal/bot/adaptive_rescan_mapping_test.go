package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

type filledMappingWriteStore struct {
	*DBMappingStore
	writes     [][]string
	failWrites int
}

func (s *filledMappingWriteStore) Write(ctx context.Context, snap StepSnapshot, resolutions []FieldResolution) error {
	refs := make([]string, len(resolutions))
	for i, resolution := range resolutions {
		refs[i] = resolution.Ref
	}
	s.writes = append(s.writes, refs)
	if s.failWrites > 0 {
		s.failWrites--
		return errors.New("temporary mapping write failure")
	}
	return s.DBMappingStore.Write(ctx, snap, resolutions)
}

func filledMappingFixture(t *testing.T) (*filledMappingWriteStore, StepSnapshot, StepInterpretation, *domain.ResumeProfile) {
	t.Helper()
	profile := groundedFixtureProfile()
	snap := makeSnap(
		SnapField{Ref: "f0", Label: "Describe your relevant experience for this role", Type: SnapTypeTextarea, Required: true, JobContext: "vacancy-a"},
		textField("f1", "Given name"),
	)
	snap.EmployerKey = "verified-issuer"
	proposal := LLMFieldProposal{
		Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: profile.Summary,
		Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}},
	}
	grounded := FieldResolution{
		Ref: "f0", Action: ActionFill, IntentType: FieldIntentLLM, Value: profile.Summary,
		Source: SourceLLM, Provenance: ProvenanceVerifiedByModel,
		grounded: newGroundedProof(profile, snap, snap.Fields[0], proposal, profile.Summary),
	}
	interp := StepInterpretation{Fields: []FieldResolution{grounded, referenceResolution("f1", "personal_information.name")}}
	store := &filledMappingWriteStore{DBMappingStore: &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}}
	return store, snap, interp, profile
}

func requireGroundedFillReusable(t *testing.T, store MappingReadWriter, snap StepSnapshot, profile *domain.ResumeProfile) {
	t.Helper()
	onlyGrounded := makeSnap(snap.Fields[0])
	onlyGrounded.EmployerKey = snap.EmployerKey
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), onlyGrounded, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, profile.Summary, warm.Fields[0].Value)
}

func TestFilledMappingLearning_NonAdvancingStepKeepsOnlyGroundedAnswer(t *testing.T) {
	for _, outcome := range []StepExecOutcome{StepOutcomeError, StepOutcomePaused, StepOutcomeSubmitted} {
		t.Run(string(outcome), func(t *testing.T) {
			store, snap, interp, profile := filledMappingFixture(t)
			result := StepExecResult{Outcome: outcome, FilledRefs: []string{"f0", "f1"}}
			if outcome == StepOutcomeError {
				result.Err = errors.New("Next did not advance")
			}
			written := writeVerifiedFilledStep(context.Background(), store, snap, interp, result)
			require.Equal(t, []string{"f0"}, written)
			writeConfirmedRemainingStep(context.Background(), store, snap, interp, result, false, written)
			require.Equal(t, [][]string{{"f0"}}, store.writes)
			requireGroundedFillReusable(t, store, snap, profile)
			_, trustedWasLearned := store.LookupField(snap.Fields[1].FieldSignature(snap.Platform), snap.EmployerKey)
			require.False(t, trustedWasLearned, "unconfirmed navigation cannot learn the non-grounded profile binding")
		})
	}
}

func TestFilledMappingLearning_ConfirmedStepWritesEachRefOnce(t *testing.T) {
	for _, outcome := range []StepExecOutcome{StepOutcomeAdvanced, StepOutcomeSubmitted} {
		t.Run(string(outcome), func(t *testing.T) {
			store, snap, interp, profile := filledMappingFixture(t)
			result := StepExecResult{Outcome: outcome, FilledRefs: []string{"f0", "f1"}}
			written := writeVerifiedFilledStep(context.Background(), store, snap, interp, result)
			writeConfirmedRemainingStep(context.Background(), store, snap, interp, result, true, written)
			require.Equal(t, [][]string{{"f0"}, {"f1"}}, store.writes)
			require.Equal(t, []string{"f0", "f1"}, result.FilledRefs, "learning must not mutate the execution result")
			requireGroundedFillReusable(t, store, snap, profile)
			_, trustedWasLearned := store.LookupField(snap.Fields[1].FieldSignature(snap.Platform), snap.EmployerKey)
			require.True(t, trustedWasLearned)
		})
	}
}

func TestFilledMappingLearning_GroundedOnlyConfirmedStepHasOneWrite(t *testing.T) {
	store, snap, interp, profile := filledMappingFixture(t)
	result := StepExecResult{Outcome: StepOutcomeAdvanced, FilledRefs: []string{"f0"}}
	written := writeVerifiedFilledStep(context.Background(), store, snap, interp, result)
	writeConfirmedRemainingStep(context.Background(), store, snap, interp, result, false, written)
	require.Equal(t, [][]string{{"f0"}}, store.writes)
	requireGroundedFillReusable(t, store, snap, profile)
}

func TestFilledMappingLearning_ConfirmedTransitionRetriesFailedCacheWrite(t *testing.T) {
	store, snap, interp, profile := filledMappingFixture(t)
	store.failWrites = 1
	result := StepExecResult{Outcome: StepOutcomeAdvanced, FilledRefs: []string{"f0", "f1"}}
	written := writeVerifiedFilledStep(context.Background(), store, snap, interp, result)
	require.Empty(t, written, "a failed cache write cannot suppress the confirmed-transition retry")
	writeConfirmedRemainingStep(context.Background(), store, snap, interp, result, false, written)
	require.Equal(t, [][]string{{"f0"}, {"f0", "f1"}}, store.writes)
	requireGroundedFillReusable(t, store, snap, profile)
}

func TestFilledMappingLearning_RequiresProofAndSuccessfulFill(t *testing.T) {
	for _, mutation := range []string{"no proof", "pending", "upload", "unfilled", "nil store"} {
		t.Run(mutation, func(t *testing.T) {
			store, snap, interp, _ := filledMappingFixture(t)
			var writer MappingReadWriter = store
			result := StepExecResult{Outcome: StepOutcomeError, FilledRefs: []string{"f0"}}
			switch mutation {
			case "no proof":
				interp.Fields[0].grounded = nil
			case "pending":
				interp.Fields[0].Pending = true
			case "upload":
				interp.Fields[0].Action = ActionUpload
			case "unfilled":
				result.FilledRefs = nil
			case "nil store":
				writer = nil
			}
			require.Empty(t, writeVerifiedFilledStep(context.Background(), writer, snap, interp, result))
			require.Empty(t, store.writes)
		})
	}
}
