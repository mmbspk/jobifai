package llmpolicy

import (
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
)

// ApplyTask returns a client configured for the given legacy or stable task key.
// Precedence: global → user legacy task_models override → approved system policy.
func ApplyTask(base *llm.Client, global domain.LLMConfig, taskModels map[string]domain.TaskModel, policy *domain.TaskModelPolicyRow, catalog *pricing.Catalog, legacyTaskKey string) (*llm.Client, error) {
	res, err := ResolveTaskModel(legacyTaskKey, global, taskModels, policy, catalog, global.Provider)
	if err != nil {
		if policy != nil && policy.State == domain.PolicyStateApproved {
			return nil, fmt.Errorf("%w: %w", ErrApprovedPolicy, err)
		}
		return nil, err
	}
	if res.Provider != global.Provider {
		return nil, fmt.Errorf("task policy provider %q must match base provider %q (cross-provider not supported yet)", res.Provider, global.Provider)
	}
	effort, err := llm.NormalizeEffort(res.Effort)
	if err != nil {
		if res.Source == "policy_approved" {
			return nil, fmt.Errorf("%w: %w", ErrApprovedPolicy, err)
		}
		return nil, err
	}
	if effort != "" && !llm.EffortSupported(global.Provider, res.Model, effort) {
		msg := fmt.Errorf("effort %q not supported for %s/%s", effort, global.Provider, res.Model)
		if res.Source == "policy_approved" {
			return nil, fmt.Errorf("%w: %w", ErrApprovedPolicy, msg)
		}
		return nil, msg
	}
	mode := res.Mode
	if mode == "" {
		mode = "pinned"
	}
	out := base.WithModel(res.Model, res.MaxTokens).WithTaskRuntime(llm.TaskRuntime{
		Effort:         effort,
		Mode:           mode,
		MaxCostUSD:     res.MaxCostUSD,
		TimeoutSec:     res.TimeoutSec,
		FallbackModels: res.FallbackModels,
	})
	if res.MaxCostUSD > 0 && catalog != nil {
		out = out.WithCostCeiling(catalog, res.MaxCostUSD)
	}
	return out, nil
}
