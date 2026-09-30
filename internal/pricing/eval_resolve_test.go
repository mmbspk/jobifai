package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveEvalPricing_GatewayActualHaiku(t *testing.T) {
	t.Parallel()
	ec := &EvalCatalog{Approved: DefaultCatalog()}
	res, meta, err := ec.ResolveEvalPricing(context.Background(), "claude", "claude-haiku-4-5-20251001", "anthropic.claude-haiku-4-5-20251001-v1:0")
	require.NoError(t, err)
	require.True(t, meta.Resolved)
	require.Equal(t, "claude-haiku-4-5", meta.CanonicalPricingModel)
	require.Equal(t, "anthropic.claude-haiku-4-5-20251001-v1:0", meta.ActualModelRaw)
	require.InDelta(t, 1.0, res.Record.InputPerM, 0.001)
}
