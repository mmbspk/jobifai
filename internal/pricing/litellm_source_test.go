package pricing

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderGuess(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "claude", ProviderGuess("claude-3-5-sonnet"))
	assert.Equal(t, "openai", ProviderGuess("gpt-5.4-mini"))
}

func TestLiteLLMSource_LastGoodFallbackCount(t *testing.T) {
	t.Parallel()
	src := &LiteLLMSource{LastGood: map[string]json.RawMessage{"a": {}, "b": {}}}
	assert.Equal(t, 2, src.fallbackCount())
}
