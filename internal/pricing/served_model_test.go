package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayCanonicalModel_HaikuBedrockID(t *testing.T) {
	t.Parallel()
	canon, ok := GatewayCanonicalModel("anthropic.claude-haiku-4-5-20251001-v1:0")
	require.True(t, ok)
	require.Equal(t, "claude-haiku-4-5", canon)
}

func TestCatalogResolve_GatewayHaikuUsesApprovedCatalog(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	res, err := c.Resolve("anthropic.claude-haiku-4-5-20251001-v1:0", true)
	require.NoError(t, err)
	require.True(t, res.Known)
	require.Equal(t, "claude-haiku-4-5", res.Record.CanonicalID)
	require.InDelta(t, 1.0, res.Record.InputPerM, 0.001)
	require.InDelta(t, 5.0, res.Record.OutputPerM, 0.001)
}

func TestLegacyPrefix_ModernHaikuDoesNotUseStaleRates(t *testing.T) {
	t.Parallel()
	_, _, ok := legacyPrefixCost("claude-haiku-4-5-20251001")
	require.False(t, ok)
	c := DefaultCatalog()
	res, err := c.Resolve("claude-haiku-4-5-20251001", true)
	require.NoError(t, err)
	require.InDelta(t, 1.0, res.Record.InputPerM, 0.001)
}
