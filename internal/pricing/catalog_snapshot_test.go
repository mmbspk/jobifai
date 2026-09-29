package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalog_AnthropicHaikuSnapshotID(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	res, err := c.Resolve("claude-haiku-4-5-20251001", false)
	require.NoError(t, err)
	require.True(t, res.Known)
	assert.NotEqual(t, LookupLegacy, res.Kind)
	assert.False(t, res.UsedFallback)
	assert.Equal(t, 1.0, res.Record.InputPerM)
	assert.Equal(t, 5.0, res.Record.OutputPerM)
	assert.Equal(t, 1.25, res.Record.CacheWrite5mPerM)
	assert.Equal(t, 2.0, res.Record.CacheWrite1hPerM)
	assert.Equal(t, 0.10, res.Record.CacheReadPerM)
}
