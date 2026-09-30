package llmpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

func TestResolveTaskModel_FormVisionFallsBackToFormFilling(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	tm := map[string]domain.TaskModel{
		"form_filling": {Model: "claude-haiku-4-5"},
	}
	res, err := ResolveTaskModel(domain.TaskFormVision, global, tm, nil, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", res.Model)
}

func TestResolveTaskModel_FormVisionDedicatedOverrideWins(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	tm := map[string]domain.TaskModel{
		"form_filling": {Model: "claude-haiku-4-5"},
		"form_vision":  {Model: "claude-sonnet-4-5"},
	}
	resAnswer, err := ResolveTaskModel(domain.TaskFormAnswer, global, tm, nil, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	resVision, err := ResolveTaskModel(domain.TaskFormVision, global, tm, nil, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", resAnswer.Model)
	assert.Equal(t, "claude-sonnet-4-5", resVision.Model)
}

func TestResolveTaskModel_ApprovedFormVisionPolicyIndependent(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	pol := &domain.TaskModelPolicyRow{
		Task:  domain.TaskFormVision,
		State: domain.PolicyStateApproved,
		Model: "claude-sonnet-4-5-20250929",
	}
	res, err := ResolveTaskModel(domain.TaskFormVision, global, nil, pol, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-5-20250929", res.Model)
	assert.Equal(t, "policy_approved", res.Source)
}
