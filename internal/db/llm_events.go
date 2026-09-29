package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// LLMUsageEventInput is a billing-safe row for llm_usage_events.
type LLMUsageEventInput struct {
	UserID            string
	Task              string
	Provider          string
	RequestedModel    string
	ActualModel       string
	InputTokens       int64
	OutputTokens      int64
	CacheWriteTokens  int64
	CacheReadTokens   int64
	RawCostUSDMicro   int64
	LoadedCostUSDMicro int64
	CreditsBurned     int64
	LatencyMS         int64
	Success           bool
	ErrorCode         string
	CorrelationID     string
	JobID             string
	ApplicationID     string
	AutomationRunID   string
	Attempt           int
	PricingSource     string
	PricingVersion    string
}

// InsertLLMUsageEvent persists one append-only billing event.
func InsertLLMUsageEvent(db *sql.DB, in LLMUsageEventInput) (string, error) {
	id := uuid.NewString()
	success := 0
	if in.Success {
		success = 1
	}
	_, err := db.Exec(`
		INSERT INTO llm_usage_events (
			id, created_at, user_id, task, provider, requested_model, actual_model,
			input_tokens, output_tokens, cache_write_tokens, cache_read_tokens,
			raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned, latency_ms,
			success, error_code, correlation_id, job_id, application_id, automation_run_id,
			attempt, pricing_source, pricing_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, time.Now().UTC(), in.UserID, in.Task, in.Provider, in.RequestedModel, in.ActualModel,
		in.InputTokens, in.OutputTokens, in.CacheWriteTokens, in.CacheReadTokens,
		in.RawCostUSDMicro, in.LoadedCostUSDMicro, in.CreditsBurned, in.LatencyMS,
		success, nullIfEmpty(in.ErrorCode), in.CorrelationID, nullIfEmpty(in.JobID),
		nullIfEmpty(in.ApplicationID), nullIfEmpty(in.AutomationRunID), in.Attempt,
		in.PricingSource, in.PricingVersion,
	)
	return id, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
