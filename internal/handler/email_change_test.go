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

	// Second consume must fail.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token="+rawToken, nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
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
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

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
	require.Equal(t, http.StatusOK, w.Code)

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

// extractQueryParam is a small helper to pull a query parameter out of a URL string.
func extractQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	v := req.URL.Query().Get(key)
	require.NotEmpty(t, v, "expected query param %q in %s", key, rawURL)
	return v
}
