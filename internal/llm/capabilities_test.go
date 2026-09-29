package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowedAnthropicEfforts_BuiltinCatalog(t *testing.T) {
	t.Parallel()
	assert.Nil(t, AllowedAnthropicEfforts("claude-haiku-4-5-20251001"))
	assert.Nil(t, AllowedAnthropicEfforts("claude-sonnet-4-5-20250929"))
	assert.NotEmpty(t, AllowedAnthropicEfforts("claude-sonnet-4-6"))
	assert.NotEmpty(t, AllowedAnthropicEfforts("claude-sonnet-5-5"))
	assert.True(t, EffortSupported("claude", "claude-sonnet-5-5", "xhigh"))
	assert.False(t, EffortSupported("claude", "claude-sonnet-4-6", "xhigh"))
	assert.False(t, EffortSupported("claude", "claude-haiku-4-5-20251001", "low"))
}
