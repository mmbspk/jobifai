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
