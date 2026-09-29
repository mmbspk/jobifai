package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyDiscovery_UnchangedKnownModel(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	ent := litellmEntry{
		InputCostPerToken:  3.0 / 1_000_000,
		OutputCostPerToken: 15.0 / 1_000_000,
		MaxInputTokens:     1_000_000,
		SupportsVision:     true,
	}
	diff, state, kind := classifyDiscovery(c, "claude", "claude-sonnet-4-6", ent, nil, nil)
	assert.Equal(t, "unchanged", state)
	assert.Equal(t, "unchanged", kind)
	assert.False(t, diff.NewModel)
	assert.Nil(t, diff.InputPrice)
}

func TestClassifyDiscovery_NewUnknownModel(t *testing.T) {
	t.Parallel()
	c := DefaultCatalog()
	ent := litellmEntry{InputCostPerToken: 0.000003, OutputCostPerToken: 0.000015}
	diff, state, kind := classifyDiscovery(c, "claude", "claude-unknown-model-xyz", ent, nil, nil)
	assert.Equal(t, "new", state)
	assert.Equal(t, "new", kind)
	assert.True(t, diff.NewModel)
}
