package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func TestResolveEffectiveLLMKey_DeploymentControlsProxyRouting(t *testing.T) {
	for _, tc := range []struct {
		name           string
		systemProxy    bool
		userProxy      bool
		personalDirect bool
		expectedKey    string
		personal       bool
	}{
		{"production uses system API key despite old Hyperspace credential", false, true, false, "system-api-key", false},
		{"production uses personal API key directly", false, true, true, "personal-api-key", true},
		{"local deployment keeps Hyperspace enabled", true, true, true, "personal-proxy-key", true},
		{"account cannot disable deployment proxy", true, false, true, "personal-proxy-key", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqldb := newTestDB(t)
			store := config.NewStore(sqldb)
			secrets := config.NewSecretsStore(sqldb, "test-passphrase")
			systemURL := ""
			if tc.systemProxy {
				systemURL = "http://localhost:6655/anthropic"
			}
			require.NoError(t, store.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
				LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: tc.systemProxy, ProxyURL: systemURL},
			}))
			require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "system-api-key"))
			require.NoError(t, secrets.Set(domain.SystemUserID, "proxy_key", "system-proxy-key"))
			const userID = "admin-with-legacy-local-settings"
			require.NoError(t, store.Set(userID, config.KeyGeneralSettings, domain.GeneralSettings{
				LLM:                 domain.LLMConfig{Provider: "claude", Model: "claude-haiku-4-5", UseProxy: true, ProxyURL: "http://localhost:6655/anthropic"},
				JobSuitabilityScore: 9,
			}))
			require.NoError(t, store.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{
				UseProxy: &tc.userProxy, ProxyURL: "http://localhost:9999/stale-proxy",
			}))
			require.NoError(t, secrets.Set(userID, "proxy_key", "personal-proxy-key"))
			if tc.personalDirect {
				require.NoError(t, secrets.Set(userID, "llm_api_key", "personal-api-key"))
			}

			gs := config.ResolveOperationalSettings(store, userID, true)
			assert.Equal(t, tc.systemProxy, gs.LLM.UseProxy)
			assert.Equal(t, systemURL, gs.LLM.ProxyURL)
			key, personal, err := config.ResolveEffectiveLLMKey(store, secrets, userID, true, &gs)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedKey, key)
			assert.Equal(t, tc.personal, personal)
			assert.Equal(t, tc.systemProxy, gs.LLM.UseProxy)
			assert.Equal(t, systemURL, gs.LLM.ProxyURL)
			assert.Equal(t, 9, gs.JobSuitabilityScore)
			if tc.personal {
				assert.Equal(t, "claude-haiku-4-5", gs.LLM.Model)
			} else {
				assert.Equal(t, "claude-sonnet-4-6", gs.LLM.Model)
			}
		})
	}
}
