package pricing

import (
	"context"
	"database/sql"
	"fmt"
)

// EvalCatalog resolves model pricing for evaluation (production catalog or staged discovery).
type EvalCatalog struct {
	Approved *Catalog
	DB       *sql.DB
}

// Resolve returns pricing for an eval candidate. Unpriced models error (no silent fallback for eval billing).
func (e *EvalCatalog) Resolve(ctx context.Context, provider, model string) (LookupResult, error) {
	if e.Approved != nil {
		res, err := e.Approved.Resolve(model, true)
		if err == nil && res.Known && !res.UsedFallback {
			if provider == "" || res.Record.Provider == provider || ProviderGuess(model) == provider {
				return res, nil
			}
		}
	}
	if e.DB == nil {
		return LookupResult{}, fmt.Errorf("eval pricing: model %q not in approved catalog", model)
	}
	var inP, outP sql.NullFloat64
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
		InputPerM: inP.Float64 * 1_000_000, OutputPerM: outP.Float64 * 1_000_000,
		SupportsVision: vision == 1, Active: true, Source: "eval_staged",
	}
	return LookupResult{Record: rec, Source: "eval_staged", Kind: LookupExact, Known: true}, nil
}
