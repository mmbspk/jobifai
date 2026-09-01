package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkedInExperienceCodes(t *testing.T) {
	codes := LinkedInExperienceCodes(ExperienceLevelConfig{
		Entry:     true,
		MidSenior: true,
		Senior:    true,
	})
	assert.Equal(t, []string{"2", "4"}, codes)

	assert.Empty(t, LinkedInExperienceCodes(ExperienceLevelConfig{}))
}

func TestMatchesExperienceTitle(t *testing.T) {
	exp := ExperienceLevelConfig{MidSenior: true, Senior: true}
	assert.True(t, exp.MatchesExperienceTitle("Software Engineer"))
	assert.True(t, exp.MatchesExperienceTitle("Senior Backend Engineer"))
	assert.False(t, exp.MatchesExperienceTitle("Intern Software Developer"))

	execOnly := ExperienceLevelConfig{Executive: true}
	assert.True(t, execOnly.MatchesExperienceTitle("Chief Technology Officer"))
	assert.False(t, execOnly.MatchesExperienceTitle("Junior Analyst"))
}

func TestLocationMatchesBlacklist(t *testing.T) {
	assert.True(t, LocationMatchesBlacklist("Perth WA", []string{"Perth"}))
	assert.True(t, LocationMatchesBlacklist("Remote - India", []string{"india"}))
	assert.False(t, LocationMatchesBlacklist("Melbourne VIC", []string{"Perth"}))
}
