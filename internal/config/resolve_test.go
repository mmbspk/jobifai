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

	got := config.ResolveOperationalSettings(store, userID, false)
	assert.Equal(t, "system-model", got.LLM.Model)
	assert.Equal(t, 9, got.JobSuitabilityScore)
	assert.Equal(t, 25, got.HumanBehavior.DailyApplicationLimit)

	require.NoError(t, store.Set(userID, config.KeyGeneralSettings, domain.GeneralSettings{
		JobSuitabilityScore: 9,
		HumanBehavior:       domain.HumanBehaviorConfig{DailyApplicationLimit: 12},
	}))
	got = config.ResolveOperationalSettings(store, userID, false)
	assert.Equal(t, 12, got.HumanBehavior.DailyApplicationLimit)

	key, err := config.ResolveLLMAPIKey(secrets, userID, false, false)
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

	got := config.ResolveOperationalSettings(store, userID, true)
	assert.Equal(t, "haiku-user", got.LLM.TaskModels["scoring"].Model)
}

// TestResolveOperationalSettings_RegularUserIgnoresLLMOverrides verifies that
// a regular user's legacy LLM config and explicit LLM overrides are both ignored
// when allowLLMOverrides=false, protecting the system provider from being replaced.
func TestResolveOperationalSettings_RegularUserIgnoresLLMOverrides(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)

	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "system-model"},
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))

	userID := "user-regular"
	// Legacy LLM on general_settings (pre-migration data).
	require.NoError(t, store.Set(userID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "openai", Model: "gpt-4o"},
		HalalJobFilter: true,
	}))
	// Explicit override key.
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "gemini", Model: "gemini-pro",
	}))

	got := config.ResolveOperationalSettings(store, userID, false)
	// LLM config must be the system default.
	assert.Equal(t, "claude", got.LLM.Provider, "regular user must not override provider")
	assert.Equal(t, "system-model", got.LLM.Model, "regular user must not override model")
	// Application preference (halal) must still be respected.
	assert.True(t, got.HalalJobFilter, "application preferences must be preserved")
}

// TestResolveOperationalSettings_TesterCanOverrideLLM verifies that a user with
// allowLLMOverrides=true has their LLM overrides applied.
func TestResolveOperationalSettings_TesterCanOverrideLLM(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)

	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "system-model"},
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))

	userID := "user-tester"
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "openai",
		Model:    "gpt-4o",
	}))

	got := config.ResolveOperationalSettings(store, userID, true)
	assert.Equal(t, "openai", got.LLM.Provider)
	assert.Equal(t, "gpt-4o", got.LLM.Model)
}

// TestResolveLLMAPIKey_ProxyKeyRequiresAllowPersonal verifies that a user-scoped
// proxy_key is not returned when allowPersonalKey=false.
func TestResolveLLMAPIKey_ProxyKeyRequiresAllowPersonal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	secrets := config.NewSecretsStore(db, "test-passphrase")

	userID := "user-proxy"
	require.NoError(t, secrets.Set(userID, "proxy_key", "user-proxy-key"))
	require.NoError(t, secrets.Set(domain.SystemUserID, "proxy_key", "system-proxy-key"))

	// Regular user: must get system proxy key, not user proxy key.
	k, err := config.ResolveLLMAPIKey(secrets, userID, false, true)
	require.NoError(t, err)
	assert.Equal(t, "system-proxy-key", k, "regular user must use system proxy key")

	// Tester: may use personal proxy key.
	k, err = config.ResolveLLMAPIKey(secrets, userID, true, true)
	require.NoError(t, err)
	assert.Equal(t, "user-proxy-key", k, "tester may use personal proxy key")
}
