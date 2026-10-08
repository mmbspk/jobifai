package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// buildRouterWithEmailSender wires an EmailSender and returns the chi router.
func buildRouterWithEmailSender(t *testing.T, sender handler.EmailSender) (http.Handler, *handler.Services) {
	t.Helper()
	svc, _ := newTestServices(t)
	svc.EmailSender = sender
	svc.AppBaseURL = "http://localhost:8081"
	return handler.NewRouter(svc), svc
}

func TestVerifyEmail_InvalidToken(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token=badtoken", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Contains(t, body["message"], "invalid")
}

func TestVerifyEmail_MissingToken(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestVerifyEmail_ValidToken_VerifiesUser(t *testing.T) {
	capture := &email.CaptureSender{}
	router, svc := buildRouterWithEmailSender(t, capture)

	// Register a user — triggers async verification email.
	body, _ := json.Marshal(map[string]string{"email": "verify@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Wait for the goroutine to deliver the email.
	rawToken := waitForEmail(t, capture)

	// Consume the token via the HTTP endpoint.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Check user is now verified in DB.
	user, err := svc.Users.ByEmail("verify@example.com")
	require.NoError(t, err)
	assert.True(t, user.EmailVerified)
}

func TestVerifyEmail_AlreadyUsed_Returns410(t *testing.T) {
	capture := &email.CaptureSender{}
	router, _ := buildRouterWithEmailSender(t, capture)

	body, _ := json.Marshal(map[string]string{"email": "used@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	rawToken := waitForEmail(t, capture)
	require.NotEmpty(t, rawToken)

	// First use — success.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Second use — should return 410 Gone.
	req3 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusGone, w3.Code)
}

func TestResendVerification_AlwaysReturns202(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	// Known address — should return 202.
	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/resend-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestResendVerification_MissingEmail_Returns400(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/auth/resend-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMe_ExposesEmailVerified(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	// Register a new user.
	regBody, _ := json.Marshal(map[string]string{"email": "me@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var tokens auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))

	// GET /api/me should include email_verified = false.
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	meW := httptest.NewRecorder()
	router.ServeHTTP(meW, meReq)
	require.Equal(t, http.StatusOK, meW.Code)
	var meBody map[string]any
	require.NoError(t, json.NewDecoder(meW.Body).Decode(&meBody))
	assert.Equal(t, false, meBody["email_verified"], "new email/password account should be unverified")

	// Manually verify the user via DB.
	us := auth.NewUserStore(db)
	user, _ := us.ByEmail("me@example.com")
	_, err := db.Exec(`UPDATE users SET email_verified = 1 WHERE id = ?`, user.ID)
	require.NoError(t, err)

	// GET /api/me should now show email_verified = true.
	meReq2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq2.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	meW2 := httptest.NewRecorder()
	router.ServeHTTP(meW2, meReq2)
	require.Equal(t, http.StatusOK, meW2.Code)
	var meBody2 map[string]any
	require.NoError(t, json.NewDecoder(meW2.Body).Decode(&meBody2))
	assert.Equal(t, true, meBody2["email_verified"])
}

// waitForEmail blocks until CaptureSender has at least one email or timeout.
func waitForEmail(t *testing.T, capture *email.CaptureSender) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if capture.Count() > 0 {
			last := capture.Last()
			if last != nil {
				return extractTokenFromURL(last.VerifyURL)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for verification email")
	return ""
}

// extractTokenFromURL parses the ?token= query param from a verify URL.
func extractTokenFromURL(verifyURL string) string {
	u, err := url.Parse(verifyURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}

// ── Dynamic email sender tests ────────────────────────────────────────────────

// TestDynamicEmailSender_NoConfigSilentSkip verifies that a dynamic sender
// returns nil (not an error) when no email configuration is stored.
func TestDynamicEmailSender_NoConfigSilentSkip(t *testing.T) {
	svc, _ := newTestServices(t)
	dynSender := handler.NewDynamicEmailSender(svc.Config, svc.Secrets)

	err := dynSender.SendVerification(context.Background(), "user@example.com", "Test", "https://x")
	assert.NoError(t, err, "unconfigured sender must return nil, not error")
}

// TestDynamicEmailSender_PicksUpConfigAfterStart verifies that a dynamic sender
// picks up email configuration stored after it was created — i.e., no restart required.
func TestDynamicEmailSender_PicksUpConfigAfterStart(t *testing.T) {
	svc, _ := newTestServices(t)
	dynSender := handler.NewDynamicEmailSender(svc.Config, svc.Secrets)

	// Phase 1: no config — must silently skip.
	err := dynSender.SendVerification(context.Background(), "user@example.com", "Test", "https://x")
	require.NoError(t, err, "should be a noop before config is set")

	// Phase 2: configure a valid-but-unreachable SMTP host to prove the
	// dynamic sender reads updated config without recreating the router.
	// Port 1 on 127.0.0.1 is always refused immediately (no timeout wait).
	require.NoError(t, svc.Config.Set(domain.SystemUserID, "email_settings", domain.EmailConfig{
		SMTPHost:  "127.0.0.1",
		SMTPPort:  1,
		EmailFrom: "noreply@test.invalid",
	}))

	// Phase 3: now the dynamic sender should attempt SMTP and return a network
	// error — proving it read the updated config without server restart.
	err = dynSender.SendVerification(context.Background(), "user@example.com", "Test", "https://x")
	require.Error(t, err, "configured but unreachable SMTP should return an error, proving config was read")
}

// TestBuildVerifyURL_UsesAppBaseURL verifies that the registered user's
// verification email contains a link anchored to the configured AppBaseURL.
func TestBuildVerifyURL_UsesAppBaseURL(t *testing.T) {
	capture := &email.CaptureSender{}
	svc, _ := newTestServices(t)
	svc.EmailSender = capture
	svc.AppBaseURL = "https://app.example.com/"
	router := handler.NewRouter(svc)

	body, _ := json.Marshal(map[string]string{"email": "urltest@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	token := waitForEmail(t, capture)
	require.NotEmpty(t, token)

	last := capture.Last()
	require.NotNil(t, last)
	assert.Contains(t, last.VerifyURL, "https://app.example.com/verify-email",
		"verify URL should use APP_BASE_URL, not localhost")
	assert.NotContains(t, last.VerifyURL, "/auth/verify-email",
		"verify URL must not use the /auth/ API prefix")
	assert.NotContains(t, last.VerifyURL, "//auth",
		"URL must not contain double-slash from trailing slash on APP_BASE_URL")
}

// TestBuildVerifyURL_FallsBackToLocalhost verifies that an empty AppBaseURL
// produces a usable localhost link (dev fallback).
func TestBuildVerifyURL_FallsBackToLocalhost(t *testing.T) {
	capture := &email.CaptureSender{}
	svc, _ := newTestServices(t)
	svc.EmailSender = capture
	svc.AppBaseURL = "" // simulate unconfigured deployment
	router := handler.NewRouter(svc)

	body, _ := json.Marshal(map[string]string{"email": "urlfall@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	token := waitForEmail(t, capture)
	require.NotEmpty(t, token)

	last := capture.Last()
	require.NotNil(t, last)
	assert.Contains(t, last.VerifyURL, "http://localhost:8081/verify-email")
	assert.NotContains(t, last.VerifyURL, "/auth/verify-email")
}

// ── Admin email API tests ─────────────────────────────────────────────────────

func TestAdminEmail_SettingsRoundTrip(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminEmail := "emailadmin@example.com"
	token := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	// GET before any config: has_smtp_pass should be false.
	wGet := authGet(t, router, "/api/admin/email/settings", token)
	require.Equal(t, http.StatusOK, wGet.Code)
	var got map[string]any
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, false, got["has_smtp_pass"])
	assert.Nil(t, got["smtp_pass"], "password must never appear in response")

	// PUT settings with smtp_pass.
	settings := map[string]any{
		"smtp_host":  "smtp.resend.com",
		"smtp_port":  587,
		"smtp_user":  "resend",
		"email_from": "noreply@example.com",
		"smtp_pass":  "secret-password",
	}
	wPut := authPut(t, router, "/api/admin/email/settings", token, settings)
	assert.Equal(t, http.StatusOK, wPut.Code)

	// GET after PUT: has_smtp_pass should be true, password not returned.
	wGet2 := authGet(t, router, "/api/admin/email/settings", token)
	require.Equal(t, http.StatusOK, wGet2.Code)
	var got2 map[string]any
	require.NoError(t, json.NewDecoder(wGet2.Body).Decode(&got2))
	assert.Equal(t, true, got2["has_smtp_pass"])
	assert.Nil(t, got2["smtp_pass"], "password must never appear in GET response")
	assert.Equal(t, "smtp.resend.com", got2["smtp_host"])
	assert.Equal(t, "resend", got2["smtp_user"])

	// DELETE smtp-pass.
	wDel := authDelete(t, router, "/api/admin/email/settings/smtp-pass", token)
	assert.Equal(t, http.StatusOK, wDel.Code)

	// GET after DELETE: has_smtp_pass should be false again.
	wGet3 := authGet(t, router, "/api/admin/email/settings", token)
	require.Equal(t, http.StatusOK, wGet3.Code)
	var got3 map[string]any
	require.NoError(t, json.NewDecoder(wGet3.Body).Decode(&got3))
	assert.Equal(t, false, got3["has_smtp_pass"])
}

func TestAdminEmail_TestEndpoint_ReturnsBadRequestWhenUnconfigured(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminEmail := "emailtest@example.com"
	token := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	wTest := authPost(t, router, "/api/admin/email/test", token, map[string]string{"to": "dest@example.com"})
	// No email config stored — should return 400 (not 500).
	assert.Equal(t, http.StatusBadRequest, wTest.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(wTest.Body).Decode(&body))
	assert.Contains(t, body["message"], "email not configured")
}
