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
