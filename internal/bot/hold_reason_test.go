package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRequireReviewHoldReason verifies that the shared helper used by both
// the LinkedIn (bot.go) and Seek (seek.go) RequireReview paths returns the
// correct message for each listing type. Both callers must use this function
// so a change to the strings breaks the test regardless of platform.
func TestRequireReviewHoldReason(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"Use Prepare documents to scan the apply form and build your application pack before approving",
		requireReviewHoldReason(true),
		"native Easy Apply listing should prompt the form-scan step",
	)
	assert.Equal(t,
		"Apply on the employer's website.",
		requireReviewHoldReason(false),
		"external listing should direct to the employer's site",
	)
}
