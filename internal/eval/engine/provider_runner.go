package engine

import (
	"context"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/providers"
	"github.com/user/jobifai/internal/eval/tasks"
	"github.com/user/jobifai/internal/llm"
)

// ProviderRunner executes production prompt paths via non-billing LLM clients.
type ProviderRunner struct {
	Factory *providers.Factory
}

func (p ProviderRunner) RunCase(ctx context.Context, task string, spec candidate.Spec, c dataset.Case) (string, llm.UsageObservation, error) {
	client, obsAccum, err := p.Factory.Client(ctx, spec)
	if err != nil {
		return "", llm.UsageObservation{}, err
	}
	out, err := tasks.Execute(ctx, task, client, c)
	obs := obsAccum.Last()
	if obs.Provider == "" {
		obs.Provider = spec.Provider
		obs.RequestedModel = spec.Model
	}
	return out, obs, err
}

