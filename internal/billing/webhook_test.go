package billing_test

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v82/webhook"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/billing"
)

func TestParseWebhookEvent_IgnoresAPIVersionMismatch(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test_secret")
	t.Setenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE", "")

	payload := []byte(`{"id":"evt_api_ver","object":"event","type":"ping","api_version":"2099-99-99.preview"}`)
	ts := time.Now()
	sig := hex.EncodeToString(webhook.ComputeSignature(ts, payload, "whsec_test_secret"))
	header := fmt.Sprintf("t=%d,v1=%s", ts.Unix(), sig)

	ev, err := billing.ParseWebhookEvent(payload, header)
	require.NoError(t, err)
	require.Equal(t, "evt_api_ver", ev.ID)
}
