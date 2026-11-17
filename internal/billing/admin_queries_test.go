package billing_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/billing"
)

// The quota page maps this response as a list. An empty result must serialize
// as [] rather than null, including when an admin filter matches no events.
func TestListWebhookEvents_EmptyResultsEncodeAsArray(t *testing.T) {
	db, _, _ := testQuota(t)
	for _, filter := range []billing.WebhookEventFilter{
		{},
		{Status: "does-not-exist"},
		{UserID: "no-such-user"},
	} {
		events, err := billing.ListWebhookEvents(context.Background(), db, filter)
		require.NoError(t, err)
		require.NotNil(t, events)
		require.Empty(t, events)
		raw, err := json.Marshal(events)
		require.NoError(t, err)
		require.Equal(t, "[]", string(raw))
	}
}
