package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

var (
	ErrWebhookNotConfigured = errors.New("stripe webhook secret not configured")
	ErrInsecureWebhookDenied = errors.New("insecure stripe webhook mode denied")
)

// ParseWebhookEvent verifies signature when configured, or accepts raw JSON only in explicit insecure test mode.
func ParseWebhookEvent(payload []byte, sigHeader string) (stripe.Event, error) {
	if IsProduction() && os.Getenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE") == "1" {
		return stripe.Event{}, ErrInsecureWebhookDenied
	}
	secret := StripeWebhookSecret()
	if secret != "" {
		return webhook.ConstructEvent(payload, sigHeader, secret)
	}
	if AllowInsecureWebhook() {
		var event stripe.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			return stripe.Event{}, err
		}
		return event, nil
	}
	return stripe.Event{}, ErrWebhookNotConfigured
}

// WebhookMissingSecretResponse is HTTP 503 for misconfigured servers.
func WebhookMissingSecretResponse() (int, string) {
	return http.StatusServiceUnavailable, "webhook not configured"
}
