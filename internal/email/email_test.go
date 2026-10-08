package email_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/email"
)

func TestRenderVerification_Plain_ContainsURL(t *testing.T) {
	plain, _, err := email.RenderVerification("Aisha Al-Rashid", "https://example.com/verify?token=abc123", false)
	require.NoError(t, err)
	assert.Contains(t, plain, "https://example.com/verify?token=abc123")
	assert.Contains(t, plain, "Aisha Al-Rashid")
	assert.NotContains(t, plain, "reminder", "initial send must not say 'reminder'")
}

func TestRenderVerification_Plain_Reminder(t *testing.T) {
	plain, _, err := email.RenderVerification("", "https://example.com/v", true)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plain), "reminder")
}

func TestRenderVerification_HTML_ContainsURL(t *testing.T) {
	_, html, err := email.RenderVerification("Bilal", "https://example.com/verify?token=xyz", false)
	require.NoError(t, err)
	assert.Contains(t, html, "https://example.com/verify?token=xyz")
	assert.Contains(t, html, "Bilal")
	// HTML must escape user input correctly.
	assert.NotContains(t, html, "<script>")
}

func TestRenderVerification_HTML_XSSEscape(t *testing.T) {
	_, html, err := email.RenderVerification(`<script>alert('xss')</script>`, "https://example.com", false)
	require.NoError(t, err)
	assert.NotContains(t, html, "<script>alert")
}

func TestNewSMTPSender_MissingHost(t *testing.T) {
	_, err := email.NewSMTPSender(email.Config{EmailFrom: "test@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "smtp_host")
}

func TestNewSMTPSender_MissingFrom(t *testing.T) {
	_, err := email.NewSMTPSender(email.Config{SMTPHost: "smtp.example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email_from")
}

func TestCaptureSender_Records(t *testing.T) {
	cs := &email.CaptureSender{}
	require.Nil(t, cs.Last())

	err := cs.SendVerification(context.Background(), "a@example.com", "Alice", "https://v1")
	require.NoError(t, err)
	assert.Equal(t, 1, cs.Count())
	assert.Equal(t, "a@example.com", cs.Last().To)
	assert.Equal(t, "https://v1", cs.Last().VerifyURL)
	assert.False(t, cs.Last().Reminder)

	err = cs.SendVerificationReminder(context.Background(), "b@example.com", "Bob", "https://v2")
	require.NoError(t, err)
	assert.Equal(t, 2, cs.Count())
	assert.Equal(t, "b@example.com", cs.Last().To)
	assert.True(t, cs.Last().Reminder)
}

func TestRenderTestEmail_SubjectAndContent(t *testing.T) {
	plain, html, err := email.RenderTestEmail()
	require.NoError(t, err)

	assert.Equal(t, "Jobifai email configuration test", email.TestEmailSubject)

	assert.Contains(t, plain, "configuration is working correctly")
	assert.Contains(t, plain, "Admin → Defaults")
	assert.Contains(t, plain, "No action is required")

	assert.Contains(t, html, "configuration is working correctly")
	assert.Contains(t, html, "Admin → Defaults")
	assert.Contains(t, html, "No action is required")
}

func TestRenderTestEmail_NoVerificationURL(t *testing.T) {
	plain, html, err := email.RenderTestEmail()
	require.NoError(t, err)

	for _, s := range []string{plain, html} {
		// Must not contain account-verification or password-reset CTAs or artifacts.
		assert.NotContains(t, s, "verify your email", "must not contain account-verification CTA")
		assert.NotContains(t, s, "verify-email", "must not contain verification URL path")
		assert.NotContains(t, s, "token=", "must not contain a token parameter")
		assert.NotContains(t, s, "example.com", "must not contain example.com")
		assert.NotContains(t, strings.ToLower(s), "reset", "must not contain password-reset wording")
		assert.NotContains(t, strings.ToLower(s), "password", "must not contain password wording")
		assert.NotContains(t, strings.ToLower(s), "account setup", "must not contain account-setup wording")
	}
}

func TestCaptureSender_TestEmail(t *testing.T) {
	cs := &email.CaptureSender{}

	err := cs.SendTestEmail(context.Background(), "admin@example.com")
	require.NoError(t, err)

	assert.Equal(t, 1, cs.Count())
	last := cs.Last()
	require.NotNil(t, last)
	assert.Equal(t, "admin@example.com", last.To)
	assert.True(t, last.ConfigTest, "ConfigTest flag must be set")
	assert.False(t, last.Reminder)
	assert.False(t, last.PasswordReset)
	assert.False(t, last.EmailChangeVerify)
	assert.Empty(t, last.VerifyURL)
	assert.Empty(t, last.ResetURL)
}
