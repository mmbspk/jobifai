package pricing

import (
	"context"
	"fmt"
	"strings"
)

// EvalPricingMeta describes how eval spend was priced for one LLM call.
type EvalPricingMeta struct {
	RequestedModel        string
	ActualModelRaw        string
	CanonicalPricingModel string
	Source                string
	Kind                  LookupKind
	Resolved              bool
	ResolveError          string
}

// ResolveEvalPricing returns catalog pricing for eval billing (strict; no silent zero on success).
func (e *EvalCatalog) ResolveEvalPricing(ctx context.Context, provider, requested, actualRaw string) (LookupResult, EvalPricingMeta, error) {
	meta := EvalPricingMeta{
		RequestedModel: strings.TrimSpace(requested),
		ActualModelRaw: strings.TrimSpace(actualRaw),
	}
	if meta.ActualModelRaw == "" {
		meta.ActualModelRaw = meta.RequestedModel
	}
	meta.CanonicalPricingModel = CatalogLookupKey(requested, meta.ActualModelRaw)
	if e == nil || e.Approved == nil {
		meta.ResolveError = "no approved catalog"
		return LookupResult{}, meta, fmt.Errorf("eval pricing: %s", meta.ResolveError)
	}
	res, err := e.Approved.Resolve(meta.CanonicalPricingModel, true)
	if err == nil && res.Known && !res.UsedFallback {
		if provider == "" || res.Record.Provider == "" || res.Record.Provider == provider || ProviderGuess(meta.CanonicalPricingModel) == provider {
			meta.Source = res.Source
			meta.Kind = res.Kind
			meta.Resolved = true
			return res, meta, nil
		}
	}
	if e.DB != nil {
		staged, sErr := e.lookupStaged(ctx, provider, meta.CanonicalPricingModel)
		if sErr == nil {
			meta.Source = staged.Source
			meta.Kind = staged.Kind
			meta.Resolved = true
			return staged, meta, nil
		}
		meta.ResolveError = sErr.Error()
	} else if err != nil {
		meta.ResolveError = err.Error()
	} else {
		meta.ResolveError = fmt.Sprintf("unpriced model %q", meta.CanonicalPricingModel)
	}
	return LookupResult{}, meta, fmt.Errorf("eval pricing: %s", meta.ResolveError)
}

func (e *EvalCatalog) lookupStaged(ctx context.Context, provider, model string) (LookupResult, error) {
	var inP, outP float64
	var vision int
	var rowProvider string
	err := e.DB.QueryRowContext(ctx, `
		SELECT provider, input_price, output_price, vision_support FROM model_catalog_candidates
		WHERE provider=? AND model=? AND eval_pricing_allowed=1 ORDER BY last_seen_at DESC LIMIT 1`,
		provider, model).Scan(&rowProvider, &inP, &outP, &vision)
	if err != nil {
		return LookupResult{}, fmt.Errorf("eval pricing: no staged price for %s/%q: %w", provider, model, err)
	}
	rec := ModelRecord{
		CanonicalID: model, Provider: rowProvider,
		InputPerM: inP * 1_000_000, OutputPerM: outP * 1_000_000,
		SupportsVision: vision == 1, Active: true, Source: "eval_staged",
	}
	return LookupResult{Record: rec, Source: "eval_staged", Kind: LookupExact, Known: true}, nil
}
