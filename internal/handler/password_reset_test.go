package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// waitForResetEmail blocks until CaptureSender has a password-reset email or times out.
// Returns the raw reset token extracted from the ResetURL.
func waitForResetEmail(t *testing.T, capture *email.CaptureSender) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if capture.Count() > 0 {
			last := capture.Last()
			if last != nil && last.PasswordReset {
				u, err := url.Parse(last.ResetURL)
				if err == nil {
					return u.Query().Get("token")
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for password reset email")
	return ""
}

func TestForgotPassword_Always202(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	// Non-existent account still returns 202.
	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestForgotPassword_SendsEmailToKnownUser(t *testing.T) {
	svc, _ := newTestServices(t)
	capture := &email.CaptureSender{}
	svc.EmailSender = capture
	router := handler.NewRouter(svc)

	// Register a user.
	token := registerAndLogin(t, router, "forgot@example.com", "password123")
	require.NotEmpty(t, token)

	body, _ := json.Marshal(map[string]string{"email": "forgot@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	rawToken := waitForResetEmail(t, capture)
	assert.NotEmpty(t, rawToken, "reset token must be extractable from email URL")
	assert.Contains(t, capture.Last().ResetURL, "/reset-password?token=")
}

func TestForgotPassword_GoogleOnlyAccountSilentSkip(t *testing.T) {
	svc, db := newTestServices(t)
	capture := &email.CaptureSender{}
	svc.EmailSender = capture
	router := handler.NewRouter(svc)

	// Insert a Google-only user (no password hash).
	store := auth.NewUserStore(db)
	_, err := store.UpsertGoogle("google-uid-reset-1", "google-reset@example.com", "Google Reset", "")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"email": "google-reset@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Give goroutine time to run; no email should appear.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, capture.Count(), "no email expected for Google-only account")
}

func TestResetPassword_Success(t *testing.T) {
	svc, _ := newTestServices(t)
	capture2 := &email.CaptureSender{}
	svc.EmailSender = capture2
	router := handler.NewRouter(svc)

	registerAndLogin(t, router, "resetpw@example.com", "oldpassword1")

	// Trigger forgot-password so a reset token is created.
	fpBody, _ := json.Marshal(map[string]string{"email": "resetpw@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(fpBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	rawToken := waitForResetEmail(t, capture2)
	require.NotEmpty(t, rawToken)

	// Use the token to reset the password.
	resetBody, _ := json.Marshal(map[string]string{
		"token":        rawToken,
		"new_password": "newpassword99",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(resetBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Should be able to log in with new password.
	loginBody, _ := json.Marshal(map[string]string{"email": "resetpw@example.com", "password": "newpassword99"})
	req3 := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestResetPassword_TokenAlreadyUsed(t *testing.T) {
	svc, _ := newTestServices(t)
	capture3 := &email.CaptureSender{}
	svc.EmailSender = capture3
	router := handler.NewRouter(svc)

	registerAndLogin(t, router, "reuse@example.com", "oldpassword1")

	fpBody, _ := json.Marshal(map[string]string{"email": "reuse@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(fpBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	rawToken := waitForResetEmail(t, capture3)

	// First use succeeds.
	body1, _ := json.Marshal(map[string]string{"token": rawToken, "new_password": "newpassword99"})
	r1 := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body1))
	r1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, r1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second use should return 410 Gone.
	body2, _ := json.Marshal(map[string]string{"token": rawToken, "new_password": "anotherpassword"})
	r2 := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body2))
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	assert.Equal(t, http.StatusGone, w2.Code)
}

func TestResetPassword_InvalidToken(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"token":        "not-a-real-token",
		"new_password": "newpassword99",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestResetPassword_PasswordTooShort(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"token":        "sometoken",
		"new_password": "short",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChangePassword_Success(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "changepw@example.com", "oldpassword1")
	require.NotEmpty(t, token)

	w := authPut(t, router, "/api/me/password", token, map[string]string{
		"current_password": "oldpassword1",
		"new_password":     "newpassword99",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Should be able to log in with new password.
	loginBody, _ := json.Marshal(map[string]string{"email": "changepw@example.com", "password": "newpassword99"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	wLogin := httptest.NewRecorder()
	router.ServeHTTP(wLogin, req)
	assert.Equal(t, http.StatusOK, wLogin.Code)
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "changepwwrong@example.com", "mypassword1")

	w := authPut(t, router, "/api/me/password", token, map[string]string{
		"current_password": "wrongpassword",
		"new_password":     "newpassword99",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestChangePassword_GoogleOnlyAccount(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tm := auth.NewTokenManager("test-jwt-secret")

	// Insert a Google-only user.
	store := auth.NewUserStore(db)
	u, err := store.UpsertGoogle("google-uid-changepw", "google-changepw@example.com", "Google ChangePW", "")
	require.NoError(t, err)

	accessToken, err := tm.IssueAccess(u.ID, u.Email)
	require.NoError(t, err)

	w := authPut(t, router, "/api/me/password", accessToken, map[string]string{
		"current_password": "",
		"new_password":     "newpassword99",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestChangePassword_Unauthenticated(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"current_password": "old",
		"new_password":     "newpassword99",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
