package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationAnswerProfileReadFailureCannotReachSubmit(t *testing.T) {
	profile := basicProfile()
	b := New(Config{Profile: profile, ProfileLoader: func() *domain.ResumeProfile { return nil }, ApplicationAnswers: &answerRecorderStub{}})
	lazy := &lazyDocGen{b: b, answerProfileDigest: profileDigest(profile)}
	executor := NewStepExecutor(basicBounds(), noGuard())
	executor.BeforeSideEffect = func(context.Context) error { return b.ensureAnswerFactsCurrent(lazy) }
	filler := &stubFiller{fillOK: true, submitReady: true}
	result, err := executor.ExecuteStep(context.Background(), snapOneField("f0"), singleFieldInterp("f0", ActionFill), filler)
	require.ErrorContains(t, err, "current application profile could not be loaded")
	require.False(t, isCandidateDeferral(err))
	require.Equal(t, StepOutcomeError, result.Outcome)
	require.Zero(t, filler.navCalls)
	require.Zero(t, filler.submitClicks)
}
