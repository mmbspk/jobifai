package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

// KeyResolver returns an API key for eval (never logged).
type KeyResolver func(provider string) (string, bool)

// Factory builds non-billing LLM clients for eval candidates.
type Factory struct {
	Catalog     *pricing.Catalog
	EvalPricing *pricing.EvalCatalog
	BaseLLM     domain.LLMConfig
	KeyResolver KeyResolver
}

// ObserverAccum collects usage observations from EvalObserver.
type ObserverAccum struct {
	mu    sync.Mutex
	last  llm.UsageObservation
}

func (o *ObserverAccum) record(u llm.UsageObservation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last = u
}

// Last returns the most recent observation.
func (o *ObserverAccum) Last() llm.UsageObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.last
}

// Client returns a configured client and observation accumulator.
func (f *Factory) Client(_ context.Context, spec candidate.Spec) (*llm.Client, *ObserverAccum, error) {
	key, ok := f.KeyResolver(spec.Provider)
	if !ok || key == "" {
		return nil, nil, fmt.Errorf("eval credentials not configured for provider %q", spec.Provider)
	}
	cfg := f.BaseLLM
	cfg.Provider = spec.Provider
	cfg.Model = spec.Model
	if spec.MaxTokens > 0 {
		cfg.MaxTokens = spec.MaxTokens
	}
	base := llm.New(cfg, key)
	acc := &ObserverAccum{}
	catalog := f.Catalog
	if catalog == nil {
		catalog = pricing.DefaultCatalog()
	}
	client := base.WithBilling(llm.BillingHooks{
		Catalog: catalog,
		EvalObserver: func(u llm.UsageObservation) {
			acc.record(u)
		},
	})
	pol := &domain.TaskModelPolicyRow{
		State: domain.PolicyStateApproved, Provider: spec.Provider, Model: spec.Model,
		MaxTokens: spec.MaxTokens, Effort: spec.Effort, TimeoutSec: spec.TimeoutSec,
	}
	out, err := llmpolicy.ApplyTask(client, cfg, nil, pol, f.Catalog, spec.Model)
	if err != nil {
		return nil, nil, err
	}
	return out, acc, nil
}
