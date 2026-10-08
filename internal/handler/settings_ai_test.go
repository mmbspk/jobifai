package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

func TestSettingsAI_RegularUserForbidden(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "user@test.com", "password123")

	w := authGet(t, router, "/api/settings/ai-provider", token)
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = authPut(t, router, "/api/settings/ai-provider", token, map[string]string{"provider": "claude"})
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = authPost(t, router, "/api/settings/secrets/api-key", token, map[string]string{"key_type": "llm_api_key", "value": "sk-test"})
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSettingsAI_TesterCanGet(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	w := authGet(t, router, "/api/settings/ai-provider", token)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "admin_default", resp["active_source"])
	assert.Equal(t, false, resp["has_key"])
}

func TestSettingsAI_TesterCanUpdateProvider(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	w := authPut(t, router, "/api/settings/ai-provider", token, map[string]string{
		"provider": "openai",
		"model":    "gpt-4o",
	})
	require.Equal(t, http.StatusOK, w.Code)

	w = authGet(t, router, "/api/settings/ai-provider", token)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "openai", resp["provider"])
	assert.Equal(t, "gpt-4o", resp["model"])
}

func TestSettingsAI_InvalidProviderRejected(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	w := authPut(t, router, "/api/settings/ai-provider", token, map[string]string{
		"provider": "notavalidprovider",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSettingsAI_TesterCanSetAndDeleteApiKey(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	// Set API key
	w := authPost(t, router, "/api/settings/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key",
		"value":    "sk-personal-tester-key",
	})
	require.Equal(t, http.StatusOK, w.Code)

	// GET should now show has_key=true and active_source=personal
	w = authGet(t, router, "/api/settings/ai-provider", token)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["has_key"])
	assert.Equal(t, "personal", resp["active_source"])

	// Delete API key
	w = authDelete(t, router, "/api/settings/secrets/api-key", token)
	require.Equal(t, http.StatusOK, w.Code)

	// GET should now show has_key=false
	w = authGet(t, router, "/api/settings/ai-provider", token)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["has_key"])
}

func TestSettingsAI_ConnectionTestNilTester(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	// LLMConnectionTester is nil — should get 503
	w := authPost(t, router, "/api/settings/ai-provider/test", token, nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
