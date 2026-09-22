package handler_test

import (
	"encoding/json"
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
