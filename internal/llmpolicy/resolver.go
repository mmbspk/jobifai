package llmpolicy

import (
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

// ResolvedTaskModel is the provider/model to use for one task.
type ResolvedTaskModel struct {
	Task           string
	Provider       string
	Model          string
	MaxTokens      int
	Effort         string
	Mode           string
	MaxCostUSD     float64
	TimeoutSec     int
	FallbackModels []string
	Source         string // global | user_override | policy_approved
}

// ResolveTaskModel picks model/provider for a stable or legacy task key.
// Precedence: global config → legacy/user task_models override → approved system policy.
func ResolveTaskModel(task string, global domain.LLMConfig, taskModels map[string]domain.TaskModel, policy *domain.TaskModelPolicyRow, catalog *pricing.Catalog, baseProvider string) (ResolvedTaskModel, error) {
	stable := domain.LegacyToStableTask(task)

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

	for _, key := range legacyTaskModelLookupKeys(stable) {
		tm, ok := taskModels[key]
		if !ok {
			continue
		}
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
		mergeTaskModelOptions(&out, tm)
		break
	}

	if policy != nil && policy.State == domain.PolicyStateApproved && policy.Model != "" {
		if policy.Provider != "" {
			out.Provider = policy.Provider
		}
		out.Model = policy.Model
		if policy.MaxTokens > 0 {
			out.MaxTokens = policy.MaxTokens
		}
		if policy.Effort != "" {
			out.Effort = policy.Effort
		}
		if policy.Mode != "" {
			out.Mode = policy.Mode
		}
		if policy.MaxCostUSD > 0 {
			out.MaxCostUSD = policy.MaxCostUSD
		}
		if policy.TimeoutSec > 0 {
			out.TimeoutSec = policy.TimeoutSec
		}
		if len(policy.FallbackModels) > 0 {
			out.FallbackModels = append([]string(nil), policy.FallbackModels...)
		}
		out.Source = "policy_approved"
	}

	if baseProvider != "" && out.Provider != "" && out.Provider != baseProvider {
		return out, fmt.Errorf("resolved provider %q does not match base %q", out.Provider, baseProvider)
	}

	// Approved policies require catalog-known pricing. Global/user overrides keep
	// legacy prefix compatibility; Ollama/local models skip cloud catalog checks.
	if catalog != nil && global.Provider != "ollama" {
		strict := out.Source == "policy_approved"
		res, err := catalog.Resolve(out.Model, strict)
		if err != nil {
			return out, err
		}
		if strict && res.UsedFallback {
			return out, pricing.ErrUnpricedModel
		}
	}
	return out, nil
}

// legacyTaskModelLookupKeys returns settings keys to try for a stable task (first match wins).
// form_vision falls back to form_filling when no dedicated override exists.
func legacyTaskModelLookupKeys(stableTask string) []string {
	switch stableTask {
	case domain.TaskFormVision:
		return []string{"form_vision", "form_filling"}
	default:
		k := domain.StableToLegacyTaskModelKey(stableTask)
		if k == "" {
			return nil
		}
		return []string{k}
	}
}

func mergeTaskModelOptions(out *ResolvedTaskModel, tm domain.TaskModel) {
	if tm.Effort != "" {
		out.Effort = tm.Effort
	}
	if tm.Mode != "" {
		out.Mode = tm.Mode
	}
	if tm.MaxCostUSD > 0 {
		out.MaxCostUSD = tm.MaxCostUSD
	}
	if tm.TimeoutSec > 0 {
		out.TimeoutSec = tm.TimeoutSec
	}
	if len(tm.FallbackModels) > 0 {
		out.FallbackModels = append([]string(nil), tm.FallbackModels...)
	}
}
