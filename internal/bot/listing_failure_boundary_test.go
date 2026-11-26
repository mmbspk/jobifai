package bot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/jobpage"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
)

func TestListingFailureMessageKeepsEvaluationAndReadingBoundaries(t *testing.T) {
	for _, cause := range []error{
		context.DeadlineExceeded,
		context.Canceled,
		quota.ErrExceeded,
		llm.ErrBillingPersistFailed,
		&jobpage.ReadError{Code: jobpage.FailureProvider, Cause: errors.New("dependency unavailable")},
	} {
		t.Run("evaluation/"+cause.Error(), func(t *testing.T) {
			code, _ := listingFailureMessage(fmt.Errorf("%w: %w", errJobEvaluationFailed, cause))
			require.Equal(t, "evaluation_failed", code)
		})
	}

	for _, test := range []struct {
		name string
		err  error
		want jobpage.FailureCode
	}{
		{"deadline", &jobpage.ReadError{Code: jobpage.FailureBrowser, Cause: context.DeadlineExceeded}, jobpage.FailureDeadline},
		{"cancellation", &jobpage.ReadError{Code: jobpage.FailureBrowser, Cause: context.Canceled}, jobpage.FailureCanceled},
		{"quota", &jobpage.ReadError{Code: jobpage.FailureProvider, Cause: quota.ErrExceeded}, jobpage.FailureQuota},
		{"billing", &jobpage.ReadError{Code: jobpage.FailureProvider, Cause: llm.ErrBillingPersistFailed}, jobpage.FailurePersistence},
		{"provider", &jobpage.ReadError{Code: jobpage.FailureProvider, Cause: errors.New("service unavailable")}, jobpage.FailureProvider},
		{"authentication", &jobpage.ReadError{Code: jobpage.FailureAuth}, jobpage.FailureAuth},
	} {
		t.Run("reading/"+test.name, func(t *testing.T) {
			code, message := listingFailureMessage(fmt.Errorf("prepare listing: %w", test.err))
			require.Equal(t, string(test.want), code)
			require.Contains(t, message, "Suitability has not been evaluated.")
		})
	}
}
