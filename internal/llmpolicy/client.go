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
		return nil, err
	}
	if res.Provider != global.Provider {
		return nil, fmt.Errorf("task policy provider %q must match base provider %q (cross-provider not supported yet)", res.Provider, global.Provider)
	}
	return base.WithModel(res.Model, res.MaxTokens), nil
}
