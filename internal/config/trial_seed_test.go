package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	appdb "github.com/user/jobifai/internal/db"
	"path/filepath"
)

func TestSeedTrialDailyApplicationLimit_SetsWhenMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	config.SeedTrialDailyApplicationLimit(store, "trial-user")

	var gs domain.GeneralSettings
	require.NoError(t, store.Get("trial-user", config.KeyGeneralSettings, &gs))
	assert.Equal(t, domain.TrialDailyApplicationLimit, gs.HumanBehavior.DailyApplicationLimit)
	assert.True(t, gs.RequireReview)
	assert.Equal(t, 7, gs.JobSuitabilityScore)
	assert.Equal(t, 25, gs.MaxJobsPerKeyword)
}

func TestSeedTrialDailyApplicationLimit_DoesNotOverwriteUserValue(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	require.NoError(t, store.Set("trial-user", config.KeyGeneralSettings, domain.GeneralSettings{
		HumanBehavior: domain.HumanBehaviorConfig{DailyApplicationLimit: 20},
	}))

	config.SeedTrialDailyApplicationLimit(store, "trial-user")

	var gs domain.GeneralSettings
	require.NoError(t, store.Get("trial-user", config.KeyGeneralSettings, &gs))
	assert.Equal(t, 20, gs.HumanBehavior.DailyApplicationLimit)
}

func TestResolveOperationalSettings_NewTrialUserEffectiveDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "system-model"},
		HumanBehavior: domain.HumanBehaviorConfig{DailyApplicationLimit: 40},
		JobSuitabilityScore: 6,
		MaxJobsPerKeyword:   30,
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))

	config.SeedTrialDailyApplicationLimit(store, "trial-user")

	got := config.ResolveOperationalSettings(store, "trial-user")
	assert.Equal(t, 7, got.JobSuitabilityScore)
	assert.Equal(t, 25, got.MaxJobsPerKeyword)
	assert.Equal(t, domain.TrialDailyApplicationLimit, got.HumanBehavior.DailyApplicationLimit)
	assert.True(t, got.RequireReview)
}
