package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

// setUserTester is a direct DB helper (no auth routing needed for setup).
func setUserTester(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	_, err := db.Exec(`UPDATE users SET is_tester = 1 WHERE email = ?`, email)
	require.NoError(t, err)
}

func TestAdminSetTester_AdminCanPromote(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	userToken := registerAndLogin(t, router, "user@test.com", "password123")
	_ = userToken

	// Get user ID
	w := authGet(t, router, "/api/admin/users", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	var userID string
	for _, r := range rows {
		if r["email"] == "user@test.com" {
			userID = r["id"].(string)
		}
	}
	require.NotEmpty(t, userID)

	// Promote to tester
	w = authPut(t, router, "/api/admin/users/"+userID, adminToken, map[string]any{"is_tester": true})
	require.Equal(t, http.StatusOK, w.Code)

	// Verify is_tester is set
	w = authGet(t, router, "/api/admin/users/"+userID, adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var detail map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&detail))
	user := detail["user"].(map[string]any)
	assert.Equal(t, true, user["is_tester"])
}

func TestAdminSetTester_DemotionCleansCredentials(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	registerAndLogin(t, router, "tester@test.com", "password123")

	// Get tester's user ID
	w := authGet(t, router, "/api/admin/users", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	var userID string
	for _, r := range rows {
		if r["email"] == "tester@test.com" {
			userID = r["id"].(string)
		}
	}
	require.NotEmpty(t, userID)

	// Set a personal API key directly via admin route
	w = authPost(t, router, "/api/admin/users/"+userID+"/secrets/api-key", adminToken, map[string]string{"value": "sk-personal-test"})
	require.Equal(t, http.StatusOK, w.Code)

	// Promote to tester
	w = authPut(t, router, "/api/admin/users/"+userID, adminToken, map[string]any{"is_tester": true})
	require.Equal(t, http.StatusOK, w.Code)

	// Demote from tester
	w = authPut(t, router, "/api/admin/users/"+userID, adminToken, map[string]any{"is_tester": false})
	require.Equal(t, http.StatusOK, w.Code)

	// Verify API key was deleted
	hasKey := svc.Secrets.Has(userID, "llm_api_key")
	assert.False(t, hasKey, "personal API key should be deleted on tester demotion")
}

func TestAdminPromoteToAdmin_ClearsTesterFlag(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	registerAndLogin(t, router, "tester@test.com", "password123")
	setUserTester(t, db, "tester@test.com")

	// Get tester user ID
	w := authGet(t, router, "/api/admin/users", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	var userID string
	for _, r := range rows {
		if r["email"] == "tester@test.com" {
			userID = r["id"].(string)
		}
	}
	require.NotEmpty(t, userID)

	// Promote to admin — should clear is_tester
	w = authPut(t, router, "/api/admin/users/"+userID, adminToken, map[string]any{"is_admin": true})
	require.Equal(t, http.StatusOK, w.Code)

	u, err := svc.Users.ByID(userID)
	require.NoError(t, err)
	assert.True(t, u.IsAdmin)
	assert.False(t, u.IsTester, "is_tester must be cleared when promoted to admin")
}

func TestAdminSetTester_RegularUserForbidden(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	_ = db
	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	regularToken := registerAndLogin(t, router, "user@test.com", "password123")

	// Get some user ID
	w := authGet(t, router, "/api/admin/users", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	var someUserID string
	for _, r := range rows {
		someUserID = r["id"].(string)
	}
	require.NotEmpty(t, someUserID)

	// Regular user cannot call admin endpoint
	w = authPut(t, router, "/api/admin/users/"+someUserID, regularToken, map[string]any{"is_tester": true})
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminAIProviderTest_NoKey(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	// No API key configured — tester should get a non-200 result or success=false
	// LLMConnectionTester is nil by default in test services
	w := authPost(t, router, "/api/admin/ai-provider/test", adminToken, nil)
	// Either 503 (tester nil) or 200 with success=false
	assert.True(t, w.Code == http.StatusServiceUnavailable || w.Code == http.StatusOK)
}

// TestAdminSetTester_SimultaneousAdminAndTesterRejected verifies that setting both
// is_admin=true and is_tester=true in a single request is rejected as a bad request.
func TestAdminSetTester_SimultaneousAdminAndTesterRejected(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminToken := registerAndLogin(t, router, "admin@test.com", "password123")
	setUserAdmin(t, db, "admin@test.com")

	registerAndLogin(t, router, "user@test.com", "password123")

	// Get user ID.
	w := authGet(t, router, "/api/admin/users", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	var userID string
	for _, r := range rows {
		if r["email"] == "user@test.com" {
			userID = r["id"].(string)
		}
	}
	require.NotEmpty(t, userID)

	// Attempt to set both admin and tester — must be rejected.
	w = authPut(t, router, "/api/admin/users/"+userID, adminToken,
		map[string]any{"is_admin": true, "is_tester": true})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
