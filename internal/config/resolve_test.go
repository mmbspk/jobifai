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

func TestResolveOperationalSettings_SystemDefaultAPIKeyScope(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "system-model"},
		HumanBehavior: domain.HumanBehaviorConfig{DailyApplicationLimit: 25},
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))

	userID := "user-a"
	require.NoError(t, store.Set(userID, config.KeyGeneralSettings, domain.GeneralSettings{
		JobSuitabilityScore: 9,
	}))

	got := config.ResolveOperationalSettings(store, userID)
	assert.Equal(t, "system-model", got.LLM.Model)
	assert.Equal(t, 9, got.JobSuitabilityScore)
	assert.Equal(t, 25, got.HumanBehavior.DailyApplicationLimit)

	key, err := config.ResolveLLMAPIKey(secrets, userID, false)
	require.NoError(t, err)
	assert.Equal(t, "sk-system", key)
}

func TestResolveOperationalSettings_UserOverridesTaskModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{
			Provider: "claude",
			Model:    "system-model",
			TaskModels: map[string]domain.TaskModel{
				"scoring": {Model: "haiku-system"},
			},
		},
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))

	userID := "user-b"
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		TaskModels: map[string]domain.TaskModel{
			"scoring": {Model: "haiku-user"},
		},
	}))

	got := config.ResolveOperationalSettings(store, userID)
	assert.Equal(t, "haiku-user", got.LLM.TaskModels["scoring"].Model)
}
