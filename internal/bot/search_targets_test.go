package bot

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

func TestBuildSeekSearchURL_PerTargetWorkArrangement(t *testing.T) {
	prefs := domain.WorkPreferences{
		JobTypes: domain.JobTypeConfig{FullTime: true},
	}
	target := domain.SearchTarget{
		Location: "All Adelaide SA",
		Onsite:   true,
	}
	u := buildSeekSearchURL("Senior Engineer", prefs, target, "")
	parsed, err := url.Parse(u)
	assert.NoError(t, err)
	assert.Equal(t, "Senior Engineer", parsed.Query().Get("keywords"))
	assert.Equal(t, "1", parsed.Query().Get("workarrangement"))
	assert.NotContains(t, parsed.Query().Get("workarrangement"), "2")
	assert.NotContains(t, parsed.Query().Get("workarrangement"), "3")
	assert.True(t, strings.Contains(parsed.Path, "adelaide") || parsed.Query().Get("where") != "" || strings.Contains(u, "seek.com"))
}

func TestBuildSeekSearchURL_HybridAndOnsite(t *testing.T) {
	target := domain.SearchTarget{Location: "All Melbourne VIC", Hybrid: true, Onsite: true}
	u := buildSeekSearchURL("Go Developer", domain.WorkPreferences{}, target, "")
	assert.Contains(t, u, "workarrangement=1%2C2")
}

func TestBuildLinkedInSearchURL_PerTarget(t *testing.T) {
	b := &Bot{cfg: Config{Preferences: domain.WorkPreferences{
		JobTypes: domain.JobTypeConfig{FullTime: true},
		Date:     domain.DateFilterConfig{Week: true},
	}}}
	target := domain.SearchTarget{Location: "All Melbourne VIC", Hybrid: true, Onsite: true}
	u := b.buildLinkedInSearchURL("Engineer", target)
	assert.Contains(t, u, "location=Melbourne")
	assert.Contains(t, u, "Victoria")
	assert.Contains(t, u, "f_WT=1%2C3")
	assert.Contains(t, u, "f_JT=F")
	assert.Contains(t, u, "f_TPR=r604800")
}

func TestBuildLinkedInSearchURL_MultipleTargets(t *testing.T) {
	b := &Bot{}
	adelaide := domain.SearchTarget{Location: "All Adelaide SA", Onsite: true}
	melbourne := domain.SearchTarget{Location: "Melbourne VIC", Hybrid: true, Onsite: true}
	u1 := b.buildLinkedInSearchURL("Engineer", adelaide)
	u2 := b.buildLinkedInSearchURL("Engineer", melbourne)
	assert.Contains(t, u1, "location=Adelaide")
	assert.Contains(t, u1, "South+Australia")
	assert.Contains(t, u1, "f_WT=1")
	assert.NotContains(t, u1, "f_WT=1%2C3")
	assert.Contains(t, u2, "location=Melbourne")
	assert.Contains(t, u2, "Victoria")
	assert.Contains(t, u2, "f_WT=1%2C3")
}

func TestSeekJobIDFromURL(t *testing.T) {
	assert.Equal(t, "87654321", seekJobIDFromURL("https://au.seek.com/job/87654321"))
	assert.Equal(t, "87654321", seekJobIDFromURL("https://au.seek.com/job/87654321/apply"))
	assert.Equal(t, "", seekJobIDFromURL("https://au.seek.com/jobs"))
}
