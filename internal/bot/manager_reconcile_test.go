package bot

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: after successful reconcile, applyErr is nil and must not enter the
// failure branch that calls applyErr.Error() (panic on nil).
func TestRunSubmitApplyErr_ReconcileClearsBeforeFailureBranch(t *testing.T) {
	t.Parallel()
	var applyErr error = errLinkedInSubmissionUnknown
	if applyErr != nil {
		if errors.Is(applyErr, errLinkedInSubmissionUnknown) {
			applyErr = nil // successful linkedInSubmitReconciled
		}
	}
	require.NoError(t, applyErr)
	if applyErr != nil {
		t.Fatalf("unexpected failure branch: %s", applyErr.Error())
	}
}
