package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestAdmin_SystemDefaults_RoundTrip(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "admin1@example.com"
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)

	body := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-test-model"},
	}
	wPost := authPut(t, router, "/api/admin/system", token, body)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/admin/system", token)
	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, "claude-test-model", got.LLM.Model)
}

func TestAdmin_UsersListAndToggleAdmin(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminEmail := "admin2@example.com"
	adminToken := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	userToken := registerAndLogin(t, router, "member@example.com", "password123")
	wMe := authGet(t, router, "/api/me", userToken)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	wList := authGet(t, router, "/api/admin/users", adminToken)
	assert.Equal(t, 200, wList.Code)

	wUp := authPut(t, router, "/api/admin/users/"+userID, adminToken, map[string]any{
		"is_admin": true,
	})
	assert.Equal(t, 200, wUp.Code)

	wMe2 := authGet(t, router, "/api/me", userToken)
	require.NoError(t, json.NewDecoder(wMe2.Body).Decode(&me))
	assert.Equal(t, true, me["is_admin"])
}

func TestAdmin_SystemPatchPreservesOtherSections(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "patch-admin@example.com", "password123")
	setUserAdmin(t, db, "patch-admin@example.com")

	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/admin/system", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusOK, patch(`{"automation":{"browser":{"show_browser":true},"human_behavior":{"daily_application_limit":42}}}`).Code)
	require.Equal(t, http.StatusOK, patch(`{"llm":{"provider":"claude","model":"test-model","task_models":{"scoring":{"model":"test-score"}}}}`).Code)

	w := authGet(t, router, "/api/admin/system", token)
	require.Equal(t, http.StatusOK, w.Code)
	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, 42, got.HumanBehavior.DailyApplicationLimit)
	assert.True(t, got.Browser.ShowBrowser)
	assert.Equal(t, "test-model", got.LLM.Model)
	assert.Equal(t, "test-score", got.LLM.TaskModels["scoring"].Model)

	// A later automation save cannot replace the AI configuration.
	require.Equal(t, http.StatusOK, patch(`{"automation":{"browser":{"show_browser":false},"human_behavior":{"daily_application_limit":21}}}`).Code)
	w = authGet(t, router, "/api/admin/system", token)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, 21, got.HumanBehavior.DailyApplicationLimit)
	assert.Equal(t, "test-model", got.LLM.Model)
	assert.Equal(t, "test-score", got.LLM.TaskModels["scoring"].Model)

	assert.Equal(t, http.StatusBadRequest, patch(`{}`).Code)
	assert.Equal(t, http.StatusBadRequest, patch(`{"llm":{},"automation":{}}`).Code)
	assert.Equal(t, http.StatusBadRequest, patch(`{"unexpected":true}`).Code)
}
