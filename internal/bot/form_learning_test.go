package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestFormLearning_OverlappingBatchesShareOnlyConcurrentMissingFields(t *testing.T) {
	coordinator := &FormLearningCoordinator{}
	first := makeSnap(textField("f0", "A"), textField("f1", "B"))
	first.EmployerKey = "issuer"
	second := makeSnap(textField("f7", "B"), textField("f8", "C"))
	second.EmployerKey = "issuer"
	calls := make(chan []string, 4)
	release := make(chan struct{})
	delegate := func(ctx context.Context, _ *domain.ResumeProfile, snap StepSnapshot, refs []string) ([]LLMFieldProposal, error) {
		calls <- append([]string{}, refs...)
		if len(refs) == 2 {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		var proposals []LLMFieldProposal
		for _, ref := range refs {
			field, _ := snap.FieldByRef(ref)
			binding := "application_defaults.notice_period"
			if field.Label == "B" {
				binding = "application_defaults.salary_expectation"
			}
			if field.Label == "C" {
				binding = "personal_information.email"
			}
			proposals = append(proposals, LLMFieldProposal{Ref: ref, Action: "fill", IntentType: FieldIntentProfile, BindingKey: binding, Value: "candidate answer must not be shared"})
		}
		return proposals, nil
	}
	type result struct {
		proposals []LLMFieldProposal
		err       error
	}
	firstResult := make(chan result, 1)
	secondResult := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		proposals, err := coordinator.Interpret(ctx, "owner/model", basicProfile(), first, []string{"f0", "f1"}, delegate)
		firstResult <- result{proposals, err}
	}()
	require.Equal(t, []string{"f0", "f1"}, <-calls)
	go func() {
		proposals, err := coordinator.Interpret(ctx, "owner/model", basicProfile(), second, []string{"f7", "f8"}, delegate)
		secondResult <- result{proposals, err}
	}()
	require.Equal(t, []string{"f8"}, <-calls, "the concurrent B field must not reach the provider twice")
	close(release)
	a, b := <-firstResult, <-secondResult
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Len(t, a.proposals, 2)
	require.Len(t, b.proposals, 2)
	assert.Equal(t, "f7", b.proposals[0].Ref)
	assert.Equal(t, "application_defaults.salary_expectation", b.proposals[0].BindingKey)
	assert.Equal(t, "f8", b.proposals[1].Ref)
	for _, proposal := range append(a.proposals, b.proposals...) {
		assert.Empty(t, proposal.Value)
	}
	_, err := coordinator.Interpret(ctx, "owner/model", basicProfile(), second, []string{"f7", "f8"}, delegate)
	require.NoError(t, err)
	assert.Equal(t, []string{"f7", "f8"}, <-calls, "a later encounter must not treat a concurrent proposal as a verified mapping")
}

func TestFormLearning_CanceledWaiterDoesNotCancelOwner(t *testing.T) {
	coordinator := &FormLearningCoordinator{}
	snap := makeSnap(textField("f0", "A"))
	entered := make(chan struct{})
	release := make(chan struct{})
	ownerDone := make(chan error, 1)
	delegate := func(ctx context.Context, _ *domain.ResumeProfile, _ StepSnapshot, _ []string) ([]LLMFieldProposal, error) {
		close(entered)
		select {
		case <-release:
			return []LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.notice_period"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, err := coordinator.Interpret(ctx, "owner", basicProfile(), snap, []string{"f0"}, delegate)
		ownerDone <- err
	}()
	<-entered
	waiter, stop := context.WithCancel(ctx)
	stop()
	_, err := coordinator.Interpret(waiter, "owner", basicProfile(), snap, []string{"f0"}, delegate)
	assert.ErrorIs(t, err, context.Canceled)
	close(release)
	require.NoError(t, <-ownerDone)
}
