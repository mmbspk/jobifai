package llmpolicy

import (
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

// ResolvedTaskModel is the provider/model to use for one task.
type ResolvedTaskModel struct {
	Task      string
	Provider  string
	Model     string
	MaxTokens int
	Source    string // global | user_override | policy_approved
}

// ResolveTaskModel picks model/provider for a stable or legacy task key.
// Precedence: global config → legacy/user task_models override → approved system policy.
func ResolveTaskModel(task string, global domain.LLMConfig, taskModels map[string]domain.TaskModel, policy *domain.TaskModelPolicyRow, catalog *pricing.Catalog, baseProvider string) (ResolvedTaskModel, error) {
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

	if legacyKey != "" {
		if tm, ok := taskModels[legacyKey]; ok {
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
	}

	if policy != nil && policy.State == domain.PolicyStateApproved && policy.Model != "" {
		if policy.Provider != "" {
			out.Provider = policy.Provider
		}
		out.Model = policy.Model
		if policy.MaxTokens > 0 {
			out.MaxTokens = policy.MaxTokens
		}
		out.Source = "policy_approved"
	}

	if baseProvider != "" && out.Provider != "" && out.Provider != baseProvider {
		return out, fmt.Errorf("resolved provider %q does not match base %q", out.Provider, baseProvider)
	}

	if catalog != nil {
		res, err := catalog.Resolve(out.Model, true)
		if err != nil {
			return out, err
		}
		if res.UsedFallback {
			return out, pricing.ErrUnpricedModel
		}
	}
	return out, nil
}
