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

func TestIsSeekHostedApplyHref_RejectsLookalikesAndEmbeddedDestinations(t *testing.T) {
	for _, raw := range []string{"https://seek.com.attacker.test/job/12345678/apply", "https://seek.com@attacker.test/job/12345678/apply", "https://attacker.test/apply?site=seek.com", "http://www.seek.com.au/job/12345678/apply"} {
		assert.False(t, isSeekHostedApplyHref(raw), raw)
		assert.True(t, isExternalApplyHref(raw), raw)
	}
	assert.Empty(t, seekJobIDFromURL("https://au.seek.com/oauth/login?returnTo=/job/12345678/apply"))
}
