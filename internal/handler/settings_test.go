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
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "sec2@example.com"
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)

	wSet := authPost(t, router, "/api/admin/system/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key", "value": "sk-real-secret",
	})
	assert.Equal(t, 200, wSet.Code)

	wGet := authGet(t, router, "/api/admin/system/secrets", token)
	var out map[string]any
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&out))
	assert.Equal(t, true, out["has_default_api_key"])
	assert.Equal(t, "****", out["llm_api_key"])
}

func TestAdmin_SystemAPIKeyForbiddenForNonAdmin(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "sec3@example.com", "password123")

	wSet := authPost(t, router, "/api/admin/system/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key", "value": "sk-real-secret",
	})
	assert.Equal(t, 403, wSet.Code)
}

func TestSettings_General_NonAdminCannotOverwriteLLM(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "gen3@example.com"
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)

	adminGS := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-admin-model"},
	}
	wAdmin := authPost(t, router, "/api/settings/general", token, adminGS)
	assert.Equal(t, 200, wAdmin.Code)

	_, err := db.Exec(`UPDATE users SET is_admin = 0 WHERE email = ?`, email)
	require.NoError(t, err)

	userGS := domain.GeneralSettings{
		JobSuitabilityScore: 8,
		LLM:                 domain.LLMConfig{Provider: "openai", Model: "gpt-hacked"},
	}
	wUser := authPost(t, router, "/api/settings/general", token, userGS)
	assert.Equal(t, 200, wUser.Code)

	wGet := authGet(t, router, "/api/settings/general", token)
	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, 8, got.JobSuitabilityScore)
	assert.Empty(t, got.LLM.Model)

	setUserAdmin(t, db, email)
	wGetAdmin := authGet(t, router, "/api/settings/general", token)
	require.NoError(t, json.NewDecoder(wGetAdmin.Body).Decode(&got))
	assert.Equal(t, "claude-admin-model", got.LLM.Model)
}
