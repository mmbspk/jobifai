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

	key, _, err := config.ResolveLLMAPIKey(secrets, userID, false, false)
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
	k, isPersonal, err := config.ResolveLLMAPIKey(secrets, userID, false, true)
	require.NoError(t, err)
	assert.Equal(t, "system-proxy-key", k, "regular user must use system proxy key")
	assert.False(t, isPersonal, "system proxy key must not be flagged as personal")

	// Tester: may use personal proxy key.
	k, isPersonal, err = config.ResolveLLMAPIKey(secrets, userID, true, true)
	require.NoError(t, err)
	assert.Equal(t, "user-proxy-key", k, "tester may use personal proxy key")
	assert.True(t, isPersonal, "user proxy key must be flagged as personal")
}

// TestResolveLLMAPIKey_PersonalKeyFlaggedCorrectly verifies the isPersonal bool for the
// non-proxy path: system key is false, user key is true.
func TestResolveLLMAPIKey_PersonalKeyFlaggedCorrectly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	secrets := config.NewSecretsStore(db, "test-passphrase")
	userID := "user-attr"

	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal"))

	// Regular user (allowPersonalKey=false) → system key, isPersonal=false.
	k, isPersonal, err := config.ResolveLLMAPIKey(secrets, userID, false, false)
	require.NoError(t, err)
	assert.Equal(t, "sk-system", k)
	assert.False(t, isPersonal, "system key must not be flagged as personal")

	// Tester (allowPersonalKey=true) → personal key, isPersonal=true.
	k, isPersonal, err = config.ResolveLLMAPIKey(secrets, userID, true, false)
	require.NoError(t, err)
	assert.Equal(t, "sk-personal", k)
	assert.True(t, isPersonal, "personal key must be flagged as personal")
}

// TestEnforceLLMCredentialConsistency_RevertsOrphanedOverride verifies that when a tester
// has a provider override but no personal key, EnforceLLMCredentialConsistency reverts
// the LLM config to the system default to prevent mixing credentials.
func TestEnforceLLMCredentialConsistency_RevertsOrphanedOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	sys := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"},
	}
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, sys))

	userID := "tester-no-key"
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "openai", Model: "gpt-4o",
	}))
	// No personal llm_api_key set for this user.

	gs := config.ResolveOperationalSettings(store, userID, true)
	// Before consistency check, override is applied.
	assert.Equal(t, "openai", gs.LLM.Provider)

	config.EnforceLLMCredentialConsistency(store, secrets, userID, true, &gs)
	assert.Equal(t, "claude", gs.LLM.Provider, "override must be reverted — no personal key")
	assert.Equal(t, "claude-sonnet-4-6", gs.LLM.Model, "model must revert to system default")
}

// TestEnforceLLMCredentialConsistency_KeepsOverrideWhenKeyPresent verifies that the
// consistency check does NOT revert LLM config when the tester has a personal key.
func TestEnforceLLMCredentialConsistency_KeepsOverrideWhenKeyPresent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"},
	}))

	userID := "tester-with-key"
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal"))
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "openai", Model: "gpt-4o",
	}))

	gs := config.ResolveOperationalSettings(store, userID, true)
	config.EnforceLLMCredentialConsistency(store, secrets, userID, true, &gs)
	assert.Equal(t, "openai", gs.LLM.Provider, "override must be kept when personal key is present")
	assert.Equal(t, "gpt-4o", gs.LLM.Model)
}

// TestEnforceLLMCredentialConsistency_TaskModelOnlyOverrideReverts verifies that a
// task-model-only override (no provider/model change) is also reverted when the tester
// has no personal key. Previously only provider/model differences triggered a revert,
// leaving task-model overrides active against the system credential.
func TestEnforceLLMCredentialConsistency_TaskModelOnlyOverrideReverts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-6",
			TaskModels: map[string]domain.TaskModel{
				"scoring": {Model: "claude-haiku-4-5"},
			},
		},
	}))
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))

	userID := "tester-task-override"
	// Override only the task model, keep same provider/model.
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		TaskModels: map[string]domain.TaskModel{
			"scoring": {Model: "gpt-4o-mini"},
		},
	}))
	// No personal key.

	gs := config.ResolveOperationalSettings(store, userID, true)
	// Task model override is applied at resolution time.
	assert.Equal(t, "gpt-4o-mini", gs.LLM.TaskModels["scoring"].Model)

	// After enforcement the system default must be fully restored.
	config.EnforceLLMCredentialConsistency(store, secrets, userID, true, &gs)
	assert.Equal(t, "claude-haiku-4-5", gs.LLM.TaskModels["scoring"].Model,
		"task-model override must be reverted — no personal key")
}

// TestEnforceLLMCredentialConsistency_PersonalKeyWithSystemProxyReverts verifies that
// when the system has proxy enabled and the tester has a personal llm_api_key but no
// personal proxy_key, the entire LLM config reverts to system. The proxy credential
// (not the direct API key) is the one that would actually be used.
func TestEnforceLLMCredentialConsistency_PersonalKeyWithSystemProxyReverts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	// System config: proxy enabled.
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{
			Provider: "claude", Model: "claude-sonnet-4-6",
			UseProxy: true, ProxyURL: "https://proxy.example.com",
		},
	}))
	require.NoError(t, secrets.Set(domain.SystemUserID, "proxy_key", "pk-system"))

	userID := "tester-proxy"
	// Tester has a personal llm_api_key but no personal proxy_key.
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal"))
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "openai", Model: "gpt-4o",
	}))

	// gs.LLM.UseProxy=true comes from system (since tester didn't override UseProxy).
	gs := config.ResolveOperationalSettings(store, userID, true)
	assert.True(t, gs.LLM.UseProxy, "proxy must be active from system config")
	assert.Equal(t, "openai", gs.LLM.Provider)

	// EnforceLLMCredentialConsistency: with UseProxy=true, ResolveLLMAPIKey checks for
	// personal proxy_key first — user has none, so system proxy key is selected
	// (isPersonal=false). The tester's direct API key is irrelevant when proxy is active.
	config.EnforceLLMCredentialConsistency(store, secrets, userID, true, &gs)
	assert.Equal(t, "claude", gs.LLM.Provider, "system credential selected — must revert to system config")
	assert.Equal(t, "claude-sonnet-4-6", gs.LLM.Model)
	assert.True(t, gs.LLM.UseProxy, "system UseProxy must be preserved")
}

// TestEnforceLLMCredentialConsistency_IncompatibleProviderKeyKept verifies that when a
// tester has a personal key AND a provider override (even if the key might not match the
// provider), the configuration is kept unchanged. Runtime auth failures propagate as
// errors and never silently fall back to the system credential — the tester owns both
// the key and the configuration.
func TestEnforceLLMCredentialConsistency_IncompatibleProviderKeyKept(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"},
	}))
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))

	userID := "tester-mismatch"
	// Tester has a personal key but overrides to a different provider.
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal-maybe-anthropic"))
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		Provider: "openai", Model: "gpt-4o",
	}))

	gs := config.ResolveOperationalSettings(store, userID, true)
	config.EnforceLLMCredentialConsistency(store, secrets, userID, true, &gs)

	// Personal key present → override kept as-is. Tester is responsible for compatibility;
	// any runtime auth error from OpenAI will propagate without fallback to system.
	assert.Equal(t, "openai", gs.LLM.Provider, "override must be kept when personal key present")
	assert.Equal(t, "gpt-4o", gs.LLM.Model)
}

// TestResolveEffectiveLLMKey_ProxyModeSwitchEdgeCase is the regression test for the
// double-resolution bug: when the tester enables UseProxy=true but has no personal proxy key,
// the system proxy credential is selected and gs.LLM reverts to system (UseProxy=false).
// A subsequent re-resolution with the reverted UseProxy=false would have found the tester's
// personal direct API key — producing a credential that differs from the one the consistency
// check based its decision on. ResolveEffectiveLLMKey must return the system direct key in
// this scenario, not the tester's personal key.
func TestResolveEffectiveLLMKey_ProxyModeSwitchEdgeCase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-passphrase")

	// System: UseProxy=false, direct API key only.
	require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: false},
	}))
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))
	require.NoError(t, secrets.Set(domain.SystemUserID, "proxy_key", "pk-system"))

	userID := "tester-proxy-switch"
	// Tester: overrides UseProxy=true, has personal direct API key, NO personal proxy key.
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal"))
	proxyTrue := true
	require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
		UseProxy: &proxyTrue,
		Provider: "openai",
		Model:    "gpt-4o",
	}))

	gs := config.ResolveOperationalSettings(store, userID, true)
	// Tester's UseProxy=true and provider override are applied.
	assert.True(t, gs.LLM.UseProxy, "tester override must set UseProxy=true")
	assert.Equal(t, "openai", gs.LLM.Provider)

	// ResolveEffectiveLLMKey probes with UseProxy=true → finds system proxy key (no personal
	// proxy key) → isPersonal=false → reverts gs.LLM to system (UseProxy=false) → resolves
	// system direct key with allowPersonalKey=false. The tester's sk-personal must NOT appear.
	key, isPersonal, err := config.ResolveEffectiveLLMKey(store, secrets, userID, true, &gs)
	require.NoError(t, err)
	assert.Equal(t, "sk-system", key, "system direct key must be selected, not tester's personal key")
	assert.False(t, isPersonal, "system credential must not be flagged as personal")
	// gs.LLM must have been reverted to the system default.
	assert.Equal(t, "claude", gs.LLM.Provider, "gs.LLM must be reverted to system provider")
	assert.False(t, gs.LLM.UseProxy, "gs.LLM.UseProxy must be reverted to system default (false)")
}
