package llmpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

func TestResolveTaskModel_LegacyInheritance(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	res, err := ResolveTaskModel("scoring", global, map[string]domain.TaskModel{}, nil, pricing.DefaultCatalog())
	require.NoError(t, err)
	assert.Equal(t, domain.TaskJobScoring, res.Task)
	assert.Equal(t, "claude-sonnet-4-6", res.Model)
	assert.Equal(t, "global", res.Source)
}

func TestResolveTaskModel_UserOverride(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	tm := map[string]domain.TaskModel{"scoring": {Model: "claude-haiku-4-5", MaxTokens: 2048}}
	res, err := ResolveTaskModel("scoring", global, tm, nil, pricing.DefaultCatalog())
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", res.Model)
	assert.Equal(t, 2048, res.MaxTokens)
}
