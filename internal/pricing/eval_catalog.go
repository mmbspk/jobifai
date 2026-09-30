package pricing

import (
	"context"
	"database/sql"
)

// EvalCatalog resolves model pricing for evaluation (production catalog or staged discovery).
type EvalCatalog struct {
	Approved *Catalog
	DB       *sql.DB
}

// Resolve returns pricing for an eval candidate (requested or gateway actual id).
func (e *EvalCatalog) Resolve(ctx context.Context, provider, model string) (LookupResult, error) {
	res, _, err := e.ResolveEvalPricing(ctx, provider, model, model)
	return res, err
}
