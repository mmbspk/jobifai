package db

import (
	"database/sql"
)

// LLMUsageEventInput is a billing-safe row for llm_usage_events.
type LLMUsageEventInput struct {
	UserID              string
	Task                string
	Provider            string
	RequestedModel      string
	ActualModel         string
	ActualModelVerified bool
	InputTokens         int64
	OutputTokens        int64
	CacheWrite5mTokens  int64
	CacheWrite1hTokens  int64
	CacheReadTokens     int64
	RawCostUSDMicro     int64
	LoadedCostUSDMicro  int64
	CreditsBurned       int64
	LatencyMS           int64
	Success             bool
	LogicalOp           bool
	ErrorCode           string
	CorrelationID       string
	IdempotencyKey      string
	JobID               string
	ApplicationID       string
	AutomationRunID     string
	Attempt             int
	PricingSource       string
	PricingVersion      string
	PersonalProvider    bool
}

// InsertLLMUsageEvent persists one event without quota (tests only).
func InsertLLMUsageEvent(db *sql.DB, in LLMUsageEventInput) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	in.LogicalOp = true
	id, err := RecordBillingTx(tx, in, 0, nil)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}
