package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSeekHostedApplyHref(t *testing.T) {
	assert.True(t, isSeekHostedApplyHref("/job/12345678/apply"))
	assert.Equal(t, "https://www.seek.com.au/job/12345678/apply", resolveSeekHref("/job/12345678/apply"))
	assert.True(t, isSeekHostedApplyHref("https://www.seek.com.au/job/12345678/apply"))
	assert.True(t, isSeekHostedApplyHref("https://www.seek.com.au/job/12345678/application"))
	assert.False(t, isSeekHostedApplyHref("https://jobs.smartrecruiters.com/apply"))
	assert.False(t, isSeekHostedApplyHref(""))
}

func TestIsExternalApplyHref(t *testing.T) {
	assert.True(t, isExternalApplyHref("https://jobs.smartrecruiters.com/oneclick-ui/company"))
	assert.False(t, isExternalApplyHref("/job/123/apply"))
	assert.False(t, isExternalApplyHref("https://www.seek.com.au/job/123/apply"))
}
