package billing

import (
	"fmt"
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

// IsProduction reports explicit production deployment (fail-closed Stripe webhook rules).
func IsProduction() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JOBIFAI_ENV"))) {
	case "production", "prod":
		return true
	default:
		return strings.EqualFold(os.Getenv("GO_ENV"), "production")
	}
}

// AllowInsecureWebhook is an explicit dev/test escape hatch (never in production).
func AllowInsecureWebhook() bool {
	if IsProduction() {
		return false
	}
	return os.Getenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE") == "1"
}

// ValidateProductionStripeConfig returns an error when production webhook settings are unsafe.
func ValidateProductionStripeConfig() error {
	if !IsProduction() {
		return nil
	}
	if os.Getenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE") == "1" {
		return fmt.Errorf("JOBIFAI_STRIPE_WEBHOOK_INSECURE must not be set in production")
	}
	if StripeWebhookSecret() == "" {
		return fmt.Errorf("STRIPE_WEBHOOK_SECRET is required in production")
	}
	return nil
}

// AppBaseURL for Checkout return URLs.
func AppBaseURL() string {
	base := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8081"
	}
	return base
}
