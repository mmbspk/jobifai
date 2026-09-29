package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/llm"
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
func (f *Factory) Client(ctx context.Context, spec candidate.Spec) (*llm.Client, *ObserverAccum, error) {
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
	evalCat := f.EvalPricing
	if evalCat == nil {
		evalCat = &pricing.EvalCatalog{Approved: catalog}
	}
	if _, err := evalCat.Resolve(ctx, spec.Model); err != nil {
		return nil, nil, fmt.Errorf("eval candidate unavailable: %w", err)
	}
	client := base.WithBilling(llm.BillingHooks{
		Catalog: catalog,
		EvalPrice: func(model string) (pricing.LookupResult, error) {
			return evalCat.Resolve(ctx, model)
		},
		EvalObserver: func(u llm.UsageObservation) {
			acc.record(u)
		},
	})
	out, err := ApplyEvalClient(client, cfg, spec)
	if err != nil {
		return nil, nil, err
	}
	return out, acc, nil
}
