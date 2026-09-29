package llmpolicy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func TestApplyTask_InvalidApprovedPolicyReturnsError(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	base := llm.New(global, "key")
	pol := &domain.TaskModelPolicyRow{
		State: domain.PolicyStateApproved,
		Model: "totally-unknown-model-xyz",
	}
	_, err := llmpolicy.ApplyTask(base, global, nil, pol, pricing.DefaultCatalog(), "scoring")
	require.Error(t, err)
	require.ErrorIs(t, err, llmpolicy.ErrApprovedPolicy)
}

func TestApplyTask_CrossProviderPolicyReturnsError(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	base := llm.New(global, "key")
	pol := &domain.TaskModelPolicyRow{
		State:    domain.PolicyStateApproved,
		Provider: "openai",
		Model:    "gpt-4o",
	}
	_, err := llmpolicy.ApplyTask(base, global, nil, pol, pricing.DefaultCatalog(), "scoring")
	require.Error(t, err)
}

func TestResolveTaskModel_OllamaSkipsStrictCatalog(t *testing.T) {
	t.Parallel()
	global := domain.LLMConfig{Provider: "ollama", Model: "llama3.2:latest"}
	_, err := llmpolicy.ResolveTaskModel("scoring", global, nil, nil, pricing.DefaultCatalog(), "ollama")
	require.NoError(t, err)
}
