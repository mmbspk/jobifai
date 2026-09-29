package pricing

import "time"

// ModelCatalogSource optionally refreshes model metadata (LiteLLM, provider API, etc.).
type ModelCatalogSource interface {
	Refresh() (addedOrUpdated int, err error)
	Name() string
}

// CatalogRefreshResult is stored in model_catalog_meta.
type CatalogRefreshResult struct {
	Source    string
	Version   string
	Refreshed time.Time
	Error     string
}

// StaticSource is the always-available built-in catalog.
type StaticSource struct{}

func (StaticSource) Name() string { return "builtin" }

func (StaticSource) Refresh() (int, error) { return 0, nil }
