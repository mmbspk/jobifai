package domain

import "time"

// ResourceType identifies a class of non-AI resource consumption.
type ResourceType string

const (
	ResourceTypeBrowserAutomation ResourceType = "browser_automation"
	ResourceTypeDocumentRender    ResourceType = "document_render"
)

// ResourceUsageEvent is one billable (or diagnostic zero-cost) resource consumption record.
// Stored in resource_usage_events. No secrets, cookies, or document body content.
type ResourceUsageEvent struct {
	ID                 string
	UserID             string
	ResourceType       ResourceType
	OperationID        string
	IdempotencyKey     string
	AutomationRunID    string
	ApplicationID      string
	JobID              string
	Quantity           float64
	Unit               string
	DurationMS         int64
	RawCostUSDMicro    int64
	LoadedCostUSDMicro int64
	CreditsBurned      int64
	Success            bool
	ErrorCode          string
	MetadataJSON       string
	CreatedAt          time.Time
}

// ResourceMeteringDefaults holds admin-configurable resource billing rates.
// Stored separately from QuotaDefaults under KV key "resource_metering_defaults".
type ResourceMeteringDefaults struct {
	// Enabled is the master gate; when false no resource credits are ever burned.
	Enabled bool                  `json:"enabled"`
	Browser BrowserMeteringConfig `json:"browser_automation"`
	Render  RenderMeteringConfig  `json:"document_render"`
}

// BrowserMeteringConfig controls active browser automation billing.
type BrowserMeteringConfig struct {
	// Enabled gates browser automation billing independently of the master toggle.
	Enabled bool `json:"enabled"`
	// RawCostUSDMicroPerHour is the raw infrastructure cost in micro-USD per browser-hour
	// (1 USD = 1_000_000 micro-USD). Loaded cost = rawMicro × (1 + ServiceMarkup).
	// Credits = loaded cost (micro-USD) × CreditsPerUSD / 1_000_000.
	// PerCallFeeUSD is NOT applied to resource billing.
	RawCostUSDMicroPerHour int64 `json:"raw_cost_usd_micro_per_hour"`
	// BillingIncrementSecs is how often a new billable bucket is opened (5–300).
	BillingIncrementSecs int `json:"billing_increment_seconds"`
}

// RenderMeteringConfig controls PDF/document render billing.
type RenderMeteringConfig struct {
	// Enabled gates render billing independently of the master toggle.
	Enabled bool `json:"enabled"`
	// RawCostUSDMicroPerRender is the raw infrastructure cost in micro-USD per successful
	// new PDF render (1 USD = 1_000_000 micro-USD).
	// Loaded cost = rawMicro × (1 + ServiceMarkup).
	// Credits = loaded cost (micro-USD) × CreditsPerUSD / 1_000_000.
	RawCostUSDMicroPerRender int64 `json:"raw_cost_usd_micro_per_render"`
}

// ResourceMeteringStatus is a stable label describing the overall resource metering readiness.
// Values: "unreviewed", "disabled", "ready", "invalid".
type ResourceMeteringStatus string

const (
	ResourceMeteringStatusUnreviewed ResourceMeteringStatus = "unreviewed"
	ResourceMeteringStatusDisabled   ResourceMeteringStatus = "disabled"
	ResourceMeteringStatusReady      ResourceMeteringStatus = "ready"
	ResourceMeteringStatusInvalid    ResourceMeteringStatus = "invalid"
)

// ResourceBrowserReadiness describes the readiness of browser automation metering.
type ResourceBrowserReadiness struct {
	Enabled                bool    `json:"enabled"`
	Ready                  bool    `json:"ready"`
	RawCostUSDMicroPerHour int64   `json:"raw_cost_usd_micro_per_hour"`
	BillingIncrementSecs   int     `json:"billing_increment_seconds"`
	CreditsPerMinute       float64 `json:"credits_per_minute"`
}

// ResourceRenderReadiness describes the readiness of document render metering.
type ResourceRenderReadiness struct {
	Enabled                  bool  `json:"enabled"`
	Ready                    bool  `json:"ready"`
	RawCostUSDMicroPerRender int64 `json:"raw_cost_usd_micro_per_render"`
	CreditsPerRender         int64 `json:"credits_per_render"`
}

// ResourceMeteringReadiness is the structured readiness contract returned by /api/admin/health.
type ResourceMeteringReadiness struct {
	DefaultsPersisted bool                     `json:"defaults_persisted"`
	MasterEnabled     bool                     `json:"master_enabled"`
	Ready             bool                     `json:"ready"`
	Status            ResourceMeteringStatus   `json:"status"`
	BrowserAutomation ResourceBrowserReadiness `json:"browser_automation"`
	DocumentRender    ResourceRenderReadiness  `json:"document_render"`
	Warnings          []string                 `json:"warnings"`
}
