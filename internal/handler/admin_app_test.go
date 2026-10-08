package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// ─── Admin app-settings endpoint tests ────────────────────────────────────

func buildAppTestRouter(t *testing.T) (http.Handler, *handler.Services, *email.CaptureSender) {
	t.Helper()
	svc, _ := newTestServices(t)
	capture := &email.CaptureSender{}
	svc.EmailSender = capture
	return handler.NewRouter(svc), svc, capture
}

func TestAppSettingsGet_DefaultsEmpty(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin@example.com")

	w := authGet(t, router, "/api/admin/app/settings", token)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp domain.ApplicationSettings
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "", resp.PublicAppURL)
}

func TestAppSettingsPut_Valid(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin2@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin2@example.com")

	w := authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": "https://jobifai.example"})
	assert.Equal(t, http.StatusOK, w.Code)

	// GET returns the saved value
	w2 := authGet(t, router, "/api/admin/app/settings", token)
	var resp domain.ApplicationSettings
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp))
	assert.Equal(t, "https://jobifai.example", resp.PublicAppURL)
}

func TestAppSettingsPut_TrailingSlashNormalized(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin3@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin3@example.com")

	authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": "https://norm.example/"})

	w := authGet(t, router, "/api/admin/app/settings", token)
	var resp domain.ApplicationSettings
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "https://norm.example", resp.PublicAppURL)
}

func TestAppSettingsPut_InvalidURLReturns422(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin4@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin4@example.com")

	w := authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": "https://x.com?q=1"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestAppSettingsPut_NonHTTPSchemeReturns422(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin5@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin5@example.com")

	w := authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": "ftp://x.com"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestAppSettingsPut_EmptyURLAccepted(t *testing.T) {
	router, svc, _ := buildAppTestRouter(t)
	token := registerAndLogin(t, router, "appadmin6@example.com", "password123")
	setUserAdmin(t, svc.DB, "appadmin6@example.com")

	// First set a value
	authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": "https://del.example"})
	// Then clear it
	w := authPut(t, router, "/api/admin/app/settings", token, map[string]string{"public_app_url": ""})
	assert.Equal(t, http.StatusOK, w.Code)

	w2 := authGet(t, router, "/api/admin/app/settings", token)
	var resp domain.ApplicationSettings
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp))
	assert.Equal(t, "", resp.PublicAppURL)
}

// ─── Email link URL resolution integration tests ──────────────────────────

// waitForVerificationEmailTo blocks until a non-reminder verification email to a specific address is captured.
func waitForVerificationEmailTo(t *testing.T, capture *email.CaptureSender, toEmail string) *email.CapturedEmail {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range capture.All() {
			if !m.Reminder && m.VerifyURL != "" && !m.EmailChangeVerify && !m.PasswordReset && m.To == toEmail {
				cp := m
				return &cp
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for verification email to %s", toEmail)
	return nil
}

// waitForPasswordResetEmail blocks until a password-reset email is captured.
func waitForPasswordResetEmail(t *testing.T, capture *email.CaptureSender) *email.CapturedEmail {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range capture.All() {
			if m.PasswordReset && m.ResetURL != "" {
				cp := m
				return &cp
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for password reset email")
	return nil
}

func TestRegistrationEmail_UsesConfiguredPublicURL(t *testing.T) {
	router, svc, capture := buildAppTestRouter(t)

	// Set persisted app URL via admin
	adminTok := registerAndLogin(t, router, "urltest-admin@example.com", "password123")
	setUserAdmin(t, svc.DB, "urltest-admin@example.com")
	w := authPut(t, router, "/api/admin/app/settings", adminTok, map[string]string{
		"public_app_url": "https://custom.jobifai.example",
	})
	require.Equal(t, http.StatusOK, w.Code)

	// Register a new user — should trigger verification email with custom URL
	body, _ := json.Marshal(map[string]string{
		"email":    "newuser-urltest@example.com",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)

	sent := waitForVerificationEmailTo(t, capture, "newuser-urltest@example.com")
	require.NotNil(t, sent)
	assert.True(t, strings.HasPrefix(sent.VerifyURL, "https://custom.jobifai.example/"),
		"expected VerifyURL to start with https://custom.jobifai.example/, got: %s", sent.VerifyURL)
}

func TestForgotPasswordEmail_UsesConfiguredPublicURL(t *testing.T) {
	router, svc, capture := buildAppTestRouter(t)

	// Register a user first
	registerAndLogin(t, router, "reset-urltest@example.com", "password123")

	// Set app URL
	adminTok := registerAndLogin(t, router, "reset-admin@example.com", "password123")
	setUserAdmin(t, svc.DB, "reset-admin@example.com")
	authPut(t, router, "/api/admin/app/settings", adminTok, map[string]string{
		"public_app_url": "https://reset.jobifai.example",
	})

	// Forgot password
	body, _ := json.Marshal(map[string]string{"email": "reset-urltest@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusAccepted, rr.Code)

	sent := waitForPasswordResetEmail(t, capture)
	require.NotNil(t, sent)
	assert.True(t, strings.HasPrefix(sent.ResetURL, "https://reset.jobifai.example/"),
		"expected ResetURL to start with https://reset.jobifai.example/, got: %s", sent.ResetURL)
}

func TestEmailChangeRequest_UsesConfiguredPublicURL(t *testing.T) {
	router, svc, capture := buildAppTestRouter(t)

	// Register + login user
	token := registerAndLogin(t, router, "changeme-urltest@example.com", "password123")

	// Set app URL
	adminTok := registerAndLogin(t, router, "change-admin@example.com", "password123")
	setUserAdmin(t, svc.DB, "change-admin@example.com")
	authPut(t, router, "/api/admin/app/settings", adminTok, map[string]string{
		"public_app_url": "https://change.jobifai.example",
	})

	// Request email change — goroutine sends email-change verification email
	authPost(t, router, "/api/me/email", token, map[string]string{
		"new_email":        "changed-urltest@example.com",
		"current_password": "password123",
	})

	// waitForEmailChangeVerify is defined in email_change_test.go and filters for EmailChangeVerify
	rawToken := waitForEmailChangeVerify(t, capture)
	assert.True(t, len(rawToken) > 0, "expected non-empty token from change-verify URL")

	// Also check the URL stored in the email
	var sentURL string
	for _, m := range capture.All() {
		if m.EmailChangeVerify {
			sentURL = m.VerifyURL
			break
		}
	}
	assert.True(t, strings.HasPrefix(sentURL, "https://change.jobifai.example/"),
		"expected change-verify URL to start with https://change.jobifai.example/, got: %s", sentURL)
}
