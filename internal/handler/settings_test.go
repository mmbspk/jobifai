package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestSettings_General_EmptyReturns200(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "gen1@example.com", "password123")

	w := authGet(t, router, "/api/settings/general", token)
	assert.Equal(t, 200, w.Code)
}

func TestSettings_General_RoundTrip(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "gen2@example.com", "password123")

	gs := domain.GeneralSettings{JobSuitabilityScore: 7, HalalJobFilter: true}
	wPost := authPost(t, router, "/api/settings/general", token, gs)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/settings/general", token)
	assert.Equal(t, 200, wGet.Code)

	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, 7, got.JobSuitabilityScore)
	assert.True(t, got.HalalJobFilter)
}

func TestSettings_Preferences_EmptyReturns200(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "pref1@example.com", "password123")

	w := authGet(t, router, "/api/settings/preferences", token)
	assert.Equal(t, 200, w.Code)
}

func TestSettings_Preferences_RoundTrip(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "pref2@example.com", "password123")

	prefs := domain.WorkPreferences{
		Positions: []string{"Data Analyst", "Business Analyst"},
		Remote:    true,
	}
	wPost := authPost(t, router, "/api/settings/preferences", token, prefs)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/settings/preferences", token)
	assert.Equal(t, 200, wGet.Code)

	var got domain.WorkPreferences
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, []string{"Data Analyst", "Business Analyst"}, got.Positions)
	assert.True(t, got.Remote)
}

func TestSettings_Secrets_EmptyResponse(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "sec1@example.com", "password123")

	w := authGet(t, router, "/api/settings/secrets", token)
	assert.Equal(t, 200, w.Code)

	var out domain.SecretsConfig
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Empty(t, out.LLMAPIKey)
	assert.Empty(t, out.CredentialPlatforms)
}

func TestSettings_Secrets_APIKeyMasked(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "sec2@example.com", "password123")

	wSet := authPost(t, router, "/api/settings/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key", "value": "sk-real-secret",
	})
	assert.Equal(t, 200, wSet.Code)

	wGet := authGet(t, router, "/api/settings/secrets", token)
	var out domain.SecretsConfig
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&out))
	// The value must be masked, not the actual plaintext.
	assert.NotEqual(t, "sk-real-secret", out.LLMAPIKey)
	assert.NotEmpty(t, out.LLMAPIKey)
}
