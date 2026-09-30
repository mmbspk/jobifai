package billing

import (
	"os"
	"strings"
)

// StripeSecretKey returns the API key used by checkout and reconciliation.
func StripeSecretKey() string {
	return strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
}

// StripeWebhookSecret returns the webhook signing secret.
func StripeWebhookSecret() string {
	return strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
}

// StripeConfigured reports whether paid flows can call Stripe.
func StripeConfigured() bool {
	return StripeSecretKey() != ""
}

// WebhookConfigured reports whether signed webhooks are expected.
func WebhookConfigured() bool {
	return StripeWebhookSecret() != ""
}

// AllowInsecureWebhook is an explicit dev/test escape hatch (never enable in production).
func AllowInsecureWebhook() bool {
	return os.Getenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE") == "1"
}

// AppBaseURL for Checkout return URLs.
func AppBaseURL() string {
	base := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8081"
	}
	return base
}
