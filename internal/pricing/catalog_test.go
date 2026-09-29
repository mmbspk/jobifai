package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalog_LookupKnownSonnet(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	rec, src, ok := c.Lookup("claude-sonnet-4-6")
	require.True(t, ok)
	assert.Equal(t, 3.0, rec.InputPerM)
	assert.Equal(t, 15.0, rec.OutputPerM)
	assert.Contains(t, src, "anthropic")
}

func TestCatalog_UnknownUsesConservativeFallback(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	rec, src, ok := c.Lookup("totally-unknown-model-xyz")
	require.False(t, ok)
	assert.Equal(t, "conservative-fallback", src)
	assert.Greater(t, rec.InputPerM, 0.0)
	micro := RawCostMicroUSD(rec, TokenUsage{InputTokens: 1000, OutputTokens: 500})
	assert.Greater(t, micro, int64(0))
}

func TestRawCostMicroUSD_CacheRead(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	rec, _, _ := c.Lookup("claude-haiku-4-5")
	micro := RawCostMicroUSD(rec, TokenUsage{InputTokens: 0, OutputTokens: 0, CacheReadTokens: 1_000_000})
	assert.InDelta(t, 0.10, float64(micro)/1_000_000, 0.0001)
}
