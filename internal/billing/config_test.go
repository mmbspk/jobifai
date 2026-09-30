package billing_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/billing"
)

func TestAllowInsecureWebhook_BlockedInProduction(t *testing.T) {
	t.Setenv("JOBIFAI_ENV", "production")
	t.Setenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE", "1")
	require.False(t, billing.AllowInsecureWebhook())
	require.Error(t, billing.ValidateProductionStripeConfig())
}

func TestParseWebhook_InsecureDeniedInProduction(t *testing.T) {
	t.Setenv("JOBIFAI_ENV", "production")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	t.Setenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE", "1")
	_, err := billing.ParseWebhookEvent([]byte(`{"id":"evt","type":"ping"}`), "")
	require.ErrorIs(t, err, billing.ErrInsecureWebhookDenied)
}
