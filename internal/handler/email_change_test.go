package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// buildEmailChangeRouter wires a test router with a CaptureSender so we can
// inspect sent emails. Returns the router, services, and capture sender.
func buildEmailChangeRouter(t *testing.T) (http.Handler, *handler.Services, *email.CaptureSender) {
	t.Helper()
	svc, _ := newTestServices(t)
	cap := &email.CaptureSender{}
	svc.EmailSender = cap
	return handler.NewRouter(svc), svc, cap
}

// waitForEmailChangeVerify polls until CaptureSender has an EmailChangeVerify
// email and returns the raw token from the VerifyURL. Times out in 2 seconds.
func waitForEmailChangeVerify(t *testing.T, cap *email.CaptureSender) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range cap.All() {
			if m.EmailChangeVerify && m.VerifyURL != "" {
				return extractQueryParam(t, m.VerifyURL, "token")
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for email-change verification email")
	return ""
}

// waitForEmailChangeNotifications polls until both the old-address notification
// and the new-address confirmation have been captured.
func waitForEmailChangeNotifications(t *testing.T, cap *email.CaptureSender) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var hasOld, hasNew bool
		for _, m := range cap.All() {
			if m.EmailChangeOldNotify {
				hasOld = true
			}
			if m.EmailChangeNewConfirm {
				hasNew = true
			}
		}
		if hasOld && hasNew {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for email-change notification emails")
}

// TestRequestEmailChange_HappyPath tests POST /api/me/email with valid credentials.
func TestRequestEmailChange_HappyPath(t *testing.T) {
	router, _, cap := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "newemail@example.com",
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	// Poll until the email-change verification email arrives.
	rawToken := waitForEmailChangeVerify(t, cap)
	assert.NotEmpty(t, rawToken)

	sent := cap.Last()
	require.NotNil(t, sent)
	assert.True(t, sent.EmailChangeVerify)
	assert.Equal(t, "newemail@example.com", sent.To)
}

// TestRequestEmailChange_WrongPassword returns 401.
func TestRequestEmailChange_WrongPassword(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "newemail@example.com",
		"current_password": "wrongpassword",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequestEmailChange_SameEmail returns 400.
func TestRequestEmailChange_SameEmail(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "user@example.com",
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestRequestEmailChange_TargetTaken returns 409 when the target email is already in use.
func TestRequestEmailChange_TargetTaken(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	registerAndLogin(t, router, "other@example.com", "password456")
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "other@example.com",
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusConflict, w.Code)
}

// TestRequestEmailChange_Unauthenticated returns 401 without a token.
func TestRequestEmailChange_Unauthenticated(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)

	w := authPost(t, router, "/api/me/email", "", map[string]string{
		"new_email":        "new@example.com",
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestVerifyEmailChange_HappyPath tests GET /auth/verify-email-change with a valid token.
func TestVerifyEmailChange_HappyPath(t *testing.T) {
	router, svc, cap := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")
	userID := userIDFromToken(t, router, token)

	// Request the change.
	w := authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "new@example.com",
		"current_password": "password123",
	})
	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the verification email and extract the raw token from the URL.
	rawToken := waitForEmailChangeVerify(t, cap)

	// Consume the verification link.
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token="+rawToken, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// User's email must be updated in the DB.
	user, err := svc.Users.ByID(userID)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", user.Email)
	assert.Empty(t, user.PendingEmail)

	// Wait for post-change notifications: 1 to old address, 1 to new address.
	waitForEmailChangeNotifications(t, cap)
	var oldNotify, newConfirm int
	for _, m := range cap.All() {
		if m.EmailChangeOldNotify {
			oldNotify++
		}
		if m.EmailChangeNewConfirm {
			newConfirm++
		}
	}
	assert.Equal(t, 1, oldNotify, "one old-address notification expected")
	assert.Equal(t, 1, newConfirm, "one new-address confirmation expected")
}

// TestVerifyEmailChange_InvalidToken returns 400 with code=invalid.
func TestVerifyEmailChange_InvalidToken(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token=nosuchtoken", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "invalid", body["code"])
}

// TestVerifyEmailChange_Replay returns 400 on second consumption.
func TestVerifyEmailChange_Replay(t *testing.T) {
	router, _, cap := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email": "new@example.com", "current_password": "password123",
	})
	rawToken := waitForEmailChangeVerify(t, cap)

	// First consume succeeds.
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token="+rawToken, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Second consume must return 410.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token="+rawToken, nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusGone, rec2.Code)
}

// TestVerifyEmailChange_MissingToken returns 400.
func TestVerifyEmailChange_MissingToken(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestDeleteAccount_WithPassword tests DELETE /api/me for email/password accounts.
func TestDeleteAccount_WithPassword(t *testing.T) {
	router, svc, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")
	userID := userIDFromToken(t, router, token)

	w := authDeleteWithBody(t, router, "/api/me", token, map[string]string{
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// User row must be gone.
	_, err := svc.Users.ByID(userID)
	assert.ErrorIs(t, err, auth.ErrUserNotFound, "user row should be deleted")
}

// TestDeleteAccount_WrongPassword returns 401.
func TestDeleteAccount_WrongPassword(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authDeleteWithBody(t, router, "/api/me", token, map[string]string{
		"current_password": "wrongpassword",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDeleteAccount_MissingPassword returns 400 for email/password accounts.
func TestDeleteAccount_MissingPassword(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	w := authDeleteWithBody(t, router, "/api/me", token, map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDeleteAccount_JWTRejectedAfterDeletion verifies that a JWT issued before
// deletion is rejected after the account is removed (RequireAuthAndExistence check).
func TestDeleteAccount_JWTRejectedAfterDeletion(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	// Delete the account.
	w := authDeleteWithBody(t, router, "/api/me", token, map[string]string{
		"current_password": "password123",
	})
	require.Equal(t, http.StatusNoContent, w.Code)

	// The previously-issued token must now be rejected.
	w2 := authGet(t, router, "/api/me", token)
	assert.Equal(t, http.StatusUnauthorized, w2.Code, "deleted account JWT must be rejected")
}

// TestDeleteAccount_Unauthenticated returns 401 without a token.
func TestDeleteAccount_Unauthenticated(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)

	w := authDeleteWithBody(t, router, "/api/me", "", map[string]string{
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMeResponse_IncludesPendingEmail checks that GET /api/me returns pending_email
// when an email change is in progress.
func TestMeResponse_IncludesPendingEmail(t *testing.T) {
	router, _, _ := buildEmailChangeRouter(t)
	token := registerAndLogin(t, router, "user@example.com", "password123")

	// Verify pending_email is null before any change.
	w := authGet(t, router, "/api/me", token)
	require.Equal(t, http.StatusOK, w.Code)
	var me map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&me))
	assert.Nil(t, me["pending_email"])

	// Request an email change.
	authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email": "pending@example.com", "current_password": "password123",
	})

	// Give the goroutine time to run (token is written synchronously before goroutine).
	// The pending_email column is set by CreateEmailChangeToken which runs synchronously.
	// Verify pending_email is now populated.
	w2 := authGet(t, router, "/api/me", token)
	require.Equal(t, http.StatusOK, w2.Code)
	var me2 map[string]any
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&me2))
	assert.Equal(t, "pending@example.com", me2["pending_email"])
}

// TestStaleJWTRejectedAfterEmailChange proves that a pre-email-change access JWT
// is rejected once the user's email has been updated (Fix 4: identity freshness).
func TestStaleJWTRejectedAfterEmailChange(t *testing.T) {
	router, svc, cap := buildEmailChangeRouter(t)

	// Step 1: log in and capture the access JWT.
	oldToken := registerAndLogin(t, router, "old@example.com", "password123")

	// Step 2: request an email change.
	w := authPost(t, router, "/api/me/email", oldToken, map[string]string{
		"new_email":        "new@example.com",
		"current_password": "password123",
	})
	require.Equal(t, http.StatusAccepted, w.Code)

	// Step 3: verify the new email (extracts raw token from the verification email).
	rawToken := waitForEmailChangeVerify(t, cap)
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token="+rawToken, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Step 4: the old access JWT must now be rejected.
	w2 := authGet(t, router, "/api/me", oldToken)
	assert.Equal(t, http.StatusUnauthorized, w2.Code, "pre-email-change JWT must be rejected after email swap")

	// Step 5: login with old email must fail.
	w3 := authPost(t, router, "/auth/login", "", map[string]string{
		"email": "old@example.com", "password": "password123",
	})
	assert.Equal(t, http.StatusUnauthorized, w3.Code, "login with old email must fail after swap")

	// Step 6: login with new email must succeed and new JWT must work.
	newToken := loginUser(t, router, "new@example.com", "password123")
	w4 := authGet(t, router, "/api/me", newToken)
	assert.Equal(t, http.StatusOK, w4.Code, "new JWT with new email must be accepted")

	_ = svc // suppress unused warning
}

// loginUser performs a POST /auth/login and returns the access token.
func loginUser(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()
	w := authPost(t, router, "/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	require.Equal(t, http.StatusOK, w.Code, "login failed: %s", w.Body)
	var tokens struct{ AccessToken string `json:"access_token"` }
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
	return tokens.AccessToken
}

// TestDeleteAccount_GoogleOnly verifies DELETE /api/me for accounts with no local
// password hash (created via Google OAuth). These accounts require the confirmation
// string "DELETE" instead of a password.
func TestDeleteAccount_GoogleOnly(t *testing.T) {
	router, svc, _ := buildEmailChangeRouter(t)

	// Create a Google-only user (no password_hash).
	u, err := svc.Users.UpsertGoogle("goog-id-test-001", "google@example.com", "Google User", "")
	require.NoError(t, err)

	// Mint a valid access token for this user.
	googleToken, err := svc.TokenManager.IssueAccess(u.ID, u.Email)
	require.NoError(t, err)

	// 1. Missing confirm entirely → 400.
	w := authDeleteWithBody(t, router, "/api/me", googleToken, map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code, "missing confirm must return 400")

	// 2. Lowercase "delete" must not be accepted → 400.
	w = authDeleteWithBody(t, router, "/api/me", googleToken, map[string]string{
		"confirm": "delete",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "lowercase confirm must return 400")

	// 3. Correct uppercase "DELETE" → 204 No Content, empty body.
	w = authDeleteWithBody(t, router, "/api/me", googleToken, map[string]string{
		"confirm": "DELETE",
	})
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Empty(t, w.Body.String(), "204 must have empty body")

	// 4. User row must be gone.
	_, err = svc.Users.ByID(u.ID)
	assert.ErrorIs(t, err, auth.ErrUserNotFound, "user row should be deleted")

	// 5. The previously-issued token must now be rejected (account no longer exists).
	w2 := authGet(t, router, "/api/me", googleToken)
	assert.Equal(t, http.StatusUnauthorized, w2.Code, "deleted Google-only account JWT must be rejected")
}

// TestDeleteAccount_HybridAccount verifies that an account with both a password_hash
// and a google_id always requires current_password — "confirm: DELETE" alone must
// not bypass the password gate.
func TestDeleteAccount_HybridAccount(t *testing.T) {
	router, svc, _ := buildEmailChangeRouter(t)

	// Create a regular email/password account, then attach a google_id.
	token := registerAndLogin(t, router, "hybrid@example.com", "password123")
	userID := userIDFromToken(t, router, token)

	_, err := svc.DB.Exec(`UPDATE users SET google_id = ? WHERE id = ?`, "goog-hybrid-456", userID)
	require.NoError(t, err)

	// Confirm string alone must not suffice for a hybrid account.
	w := authDeleteWithBody(t, router, "/api/me", token, map[string]string{
		"confirm": "DELETE",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "hybrid account must require password, not confirm string")

	// Correct password must succeed.
	w = authDeleteWithBody(t, router, "/api/me", token, map[string]string{
		"current_password": "password123",
	})
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	_, err = svc.Users.ByID(userID)
	assert.ErrorIs(t, err, auth.ErrUserNotFound, "hybrid account row should be deleted")
}

// extractQueryParam is a small helper to pull a query parameter out of a URL string.
func extractQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	v := req.URL.Query().Get(key)
	require.NotEmpty(t, v, "expected query param %q in %s", key, rawURL)
	return v
}
