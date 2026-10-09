package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrIdempotencyConflict indicates this logical operation was already billed.
var ErrIdempotencyConflict = errors.New("llm usage idempotency key already recorded")

// RecordBillingTx atomically inserts the event, updates usage_totals, and applies quota burn SQL.
func RecordBillingTx(tx *sql.Tx, in LLMUsageEventInput, quotaBurnCredits int64, quotaUpdate func(*sql.Tx, string, int64) error) (string, error) {
	if in.UserID == "" {
		return "", errors.New("billing: missing user_id")
	}
	if in.IdempotencyKey != "" {
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM llm_usage_events WHERE idempotency_key = ? LIMIT 1`, in.IdempotencyKey).Scan(&exists)
		if err == nil {
			return "", ErrIdempotencyConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	id := uuid.NewString()
	success := 0
	if in.Success {
		success = 1
	}
	verified := 0
	if in.ActualModelVerified {
		verified = 1
	}
	personalProvider := 0
	if in.PersonalProvider {
		personalProvider = 1
	}
	_, err := tx.Exec(`
		INSERT INTO llm_usage_events (
			id, created_at, user_id, task, provider, requested_model, actual_model, actual_model_verified,
			input_tokens, output_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_read_tokens,
			raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned, latency_ms,
			success, error_code, correlation_id, idempotency_key, job_id, application_id, automation_run_id,
			attempt, pricing_source, pricing_version, personal_provider
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, time.Now().UTC(), in.UserID, in.Task, in.Provider, in.RequestedModel, in.ActualModel, verified,
		in.InputTokens, in.OutputTokens, in.CacheWrite5mTokens, in.CacheWrite1hTokens, in.CacheReadTokens,
		in.RawCostUSDMicro, in.LoadedCostUSDMicro, in.CreditsBurned, in.LatencyMS,
		success, nullIfEmpty(in.ErrorCode), in.CorrelationID, nullIfEmpty(in.IdempotencyKey),
		nullIfEmpty(in.JobID), nullIfEmpty(in.ApplicationID), nullIfEmpty(in.AutomationRunID),
		in.Attempt, in.PricingSource, in.PricingVersion, personalProvider,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrIdempotencyConflict
		}
		return "", err
	}
	if in.Success && in.LogicalOp && (in.InputTokens > 0 || in.OutputTokens > 0) {
		_, err = tx.Exec(`
			INSERT INTO usage_totals (user_id, model, input_tokens, output_tokens, calls, updated_at)
			VALUES (?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(user_id, model) DO UPDATE SET
				input_tokens  = input_tokens  + excluded.input_tokens,
				output_tokens = output_tokens + excluded.output_tokens,
				calls         = calls         + 1,
				updated_at    = CURRENT_TIMESTAMP`,
			in.UserID, in.ActualModel, in.InputTokens, in.OutputTokens,
		)
		if err != nil {
			return "", err
		}
	}
	if in.Success && in.LogicalOp && quotaBurnCredits > 0 && quotaUpdate != nil {
		if err := quotaUpdate(tx, in.UserID, quotaBurnCredits); err != nil {
			return "", err
		}
	}
	return id, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
