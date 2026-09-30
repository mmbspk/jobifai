package providers

import (
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/llm"
)

// ApplyEvalClient configures runtime for evaluation without production-catalog strictness.
func ApplyEvalClient(base *llm.Client, global domain.LLMConfig, spec candidate.Spec) (*llm.Client, error) {
	if spec.Provider != global.Provider {
		return nil, fmt.Errorf("eval provider %q must match base %q", spec.Provider, global.Provider)
	}
	effort, err := llm.NormalizeEffort(global.Provider, spec.Effort)
	if err != nil {
		return nil, err
	}
	if effort != "" && !llm.EffortSupported(global.Provider, spec.Model, effort) {
		return nil, fmt.Errorf("effort %q unsupported for %s/%s", spec.Effort, spec.Provider, spec.Model)
	}
	maxTok := spec.MaxTokens
	if maxTok <= 0 {
		maxTok = global.MaxTokens
	}
	out := base.WithModel(spec.Model, maxTok).WithTaskRuntime(llm.TaskRuntime{
		Effort: effort, Mode: "pinned", TimeoutSec: spec.TimeoutSec,
	})
	return out, nil
}
