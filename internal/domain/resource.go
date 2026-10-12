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
	// RawCostUSDPerHour is the raw infrastructure cost assumption per browser-hour.
	// Loaded cost = RawCostUSDPerHour × (1 + ServiceMarkup).
	// Credits = loaded cost × CreditsPerUSD.
	// PerCallFeeUSD is NOT applied.
	RawCostUSDPerHour float64 `json:"raw_cost_usd_per_hour"`
	// BillingIncrementSecs is how often a new billable bucket is opened (5–300).
	BillingIncrementSecs int `json:"billing_increment_seconds"`
}

// RenderMeteringConfig controls PDF/document render billing.
type RenderMeteringConfig struct {
	// Enabled gates render billing independently of the master toggle.
	Enabled bool `json:"enabled"`
	// RawCostUSDPerRender is the raw infrastructure cost per successful new PDF render.
	// Loaded cost = RawCostUSDPerRender × (1 + ServiceMarkup).
	// Credits = loaded cost × CreditsPerUSD.
	RawCostUSDPerRender float64 `json:"raw_cost_usd_per_render"`
}
