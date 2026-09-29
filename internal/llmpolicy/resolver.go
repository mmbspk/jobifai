package llmpolicy

import (
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

// ResolvedTaskModel is the provider/model to use for one task.
type ResolvedTaskModel struct {
	Task       string
	Provider   string
	Model      string
	MaxTokens  int
	Source     string // user_override | global | policy_approved
}

// ResolveTaskModel picks model/provider for a stable or legacy task key.
// Approved task_model_policies rows override user settings when state is approved.
func ResolveTaskModel(task string, global domain.LLMConfig, taskModels map[string]domain.TaskModel, policy *domain.TaskModelPolicyRow, catalog *pricing.Catalog) (ResolvedTaskModel, error) {
	stable := domain.LegacyToStableTask(task)
	legacyKey := domain.StableToLegacyTaskModelKey(stable)

	out := ResolvedTaskModel{
		Task:      stable,
		Provider:  global.Provider,
		Model:     global.Model,
		MaxTokens: global.MaxTokens,
		Source:    "global",
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = 8192
	}

	if policy != nil && policy.State == domain.PolicyStateApproved && policy.Model != "" {
		out.Model = policy.Model
		if policy.Provider != "" {
			out.Provider = policy.Provider
		}
		if policy.MaxTokens > 0 {
			out.MaxTokens = policy.MaxTokens
		}
		out.Source = "policy_approved"
	} else if tm, ok := taskModels[legacyKey]; ok {
		if tm.Provider != "" {
			out.Provider = tm.Provider
		}
		if tm.Model != "" {
			out.Model = tm.Model
			out.Source = "user_override"
		}
		if tm.MaxTokens > 0 {
			out.MaxTokens = tm.MaxTokens
		}
	}

	if catalog != nil {
		if _, _, ok := catalog.Lookup(out.Model); !ok {
			return out, pricing.ErrUnpricedModel
		}
	}
	return out, nil
}
