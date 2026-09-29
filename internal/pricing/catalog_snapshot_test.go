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

func TestCatalog_SpeculativeSonnet46SnapshotNotAliased(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	res, err := c.Resolve("claude-sonnet-4-6-20250514", false)
	require.NoError(t, err)
	assert.NotEqual(t, LookupExact, res.Kind)
	assert.NotEqual(t, LookupAlias, res.Kind)
}

func TestCatalog_DocumentedAliasesOnly(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	for _, id := range []string{
		"claude-haiku-4-5-20251001",
		"claude-sonnet-4-5-20250929",
	} {
		res, err := c.Resolve(id, false)
		require.NoError(t, err)
		assert.True(t, res.Known, id)
		assert.Equal(t, LookupAlias, res.Kind, id)
	}
}
