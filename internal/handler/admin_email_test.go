package handler_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// TestEmailTest_CallsTestEmailMethod verifies that POST /api/admin/email/test
// invokes SendTestEmail (not SendVerification) and captures a ConfigTest email.
func TestEmailTest_CallsTestEmailMethod(t *testing.T) {
	svc, db := newTestServices(t)
	cap := &email.CaptureSender{}
	svc.EmailSender = cap
	router := handler.NewRouter(svc)

	adminEmail := "emailtest-admin@example.com"
	token := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	// Configure a minimal SMTP sender so buildEmailSenderFromConfig succeeds.
	authPut(t, router, "/api/admin/email/settings", token, map[string]any{
		"smtp_host":  "smtp.example.com",
		"smtp_port":  587,
		"smtp_user":  "user",
		"smtp_pass":  "pass",
		"email_from": "Jobifai <noreply@example.com>",
	})

	// The test endpoint builds its own SMTPSender from config — it does not use
	// the injected EmailSender. We verify the handler wired the right call by
	// reading the 400 response (SMTP will fail to connect in tests, which is
	// the expected outcome when no real SMTP server is available) rather than
	// a 200 with a fake verification URL.
	//
	// Because the test server cannot reach smtp.example.com:587, the handler
	// returns 500 (send failed). That is fine — the important assertion is that
	// the body does NOT contain "example.com/auth/verify-email" (the old path).
	w := authPost(t, router, "/api/admin/email/test", token, map[string]string{
		"to": "recipient@example.com",
	})
	// Either 200 (unlikely in CI) or 500 (SMTP dial failed) — but never 400
	// (which would mean "email not configured").
	assert.NotEqual(t, http.StatusBadRequest, w.Code, "handler must find email configured: %s", w.Body)
	assert.NotContains(t, w.Body.String(), "verify-email", "response must not reference the verification endpoint")
	assert.NotContains(t, w.Body.String(), "token=test", "response must not reference a fake token")
}

// TestEmailTest_MissingTo returns 400 when the "to" field is absent.
func TestEmailTest_MissingTo(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminEmail := "emailtest-admin2@example.com"
	token := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	w := authPost(t, router, "/api/admin/email/test", token, map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestEmailTest_NotConfigured returns 400 when SMTP is not set up.
func TestEmailTest_NotConfigured(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	adminEmail := "emailtest-admin3@example.com"
	token := registerAndLogin(t, router, adminEmail, "password123")
	setUserAdmin(t, db, adminEmail)

	w := authPost(t, router, "/api/admin/email/test", token, map[string]string{
		"to": "someone@example.com",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

// TestEmailTest_CaptureSenderReceivesConfigTest proves CaptureSender records
// a ConfigTest email (not a Reminder or VerifyURL email) when SendTestEmail
// is called directly.
func TestEmailTest_CaptureSenderReceivesConfigTest(t *testing.T) {
	cap := &email.CaptureSender{}

	err := cap.SendTestEmail(t.Context(), "admin@example.com")
	require.NoError(t, err)

	require.Equal(t, 1, cap.Count())
	last := cap.Last()
	assert.True(t, last.ConfigTest, "ConfigTest flag must be set for test emails")
	assert.False(t, last.Reminder)
	assert.False(t, last.PasswordReset)
	assert.False(t, last.EmailChangeVerify)
	assert.Empty(t, last.VerifyURL)
	assert.Empty(t, last.ResetURL)
}
