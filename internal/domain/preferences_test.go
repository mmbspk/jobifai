package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkPreferences_Normalize_FromLegacyLocations(t *testing.T) {
	p := WorkPreferences{
		Remote:    true,
		Hybrid:    false,
		Onsite:    true,
		Locations: []string{"All Adelaide SA", "All Melbourne VIC"},
	}
	p.Normalize()

	assert.Len(t, p.SearchTargets, 2)
	assert.Equal(t, "All Adelaide SA", p.SearchTargets[0].Location)
	assert.True(t, p.SearchTargets[0].Onsite)
	assert.False(t, p.SearchTargets[0].Hybrid)
	assert.Equal(t, "All Melbourne VIC", p.SearchTargets[1].Location)
	assert.Equal(t, []string{"All Adelaide SA", "All Melbourne VIC"}, p.Locations)
}

func TestWorkPreferences_Normalize_PreservesSearchTargets(t *testing.T) {
	p := WorkPreferences{
		SearchTargets: []SearchTarget{
			{Location: "All Adelaide SA", Onsite: true},
			{Location: "All Melbourne VIC", Hybrid: true, Onsite: true},
		},
		Positions: []string{"Senior Engineer"},
	}
	p.Normalize()

	assert.Len(t, p.SearchTargets, 2)
	assert.True(t, p.SearchTargets[0].Onsite)
	assert.False(t, p.SearchTargets[0].Hybrid)
	assert.True(t, p.SearchTargets[1].Hybrid)
	assert.True(t, p.SearchTargets[1].Onsite)
}

func TestWorkPreferences_EffectiveSearchTargets_DefaultNationwide(t *testing.T) {
	targets := (WorkPreferences{}).EffectiveSearchTargets()
	assert.Len(t, targets, 1)
	assert.Equal(t, "", targets[0].Location)
}
