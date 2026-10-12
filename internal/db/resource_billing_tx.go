package db

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ErrResourceIdempotencyConflict indicates this resource event was already recorded.
var ErrResourceIdempotencyConflict = errors.New("resource usage idempotency key already recorded")

// RecordResourceBillingTx atomically burns quota credits and inserts a resource usage event.
// Quota burn happens FIRST so that credits_burned in the event row reflects the actual
// deduction (0 for admin/enforcement-disabled paths, not the requested amount).
//
// If idempotency_key is non-empty and already recorded, returns ErrResourceIdempotencyConflict
// so the caller can commit a no-op (credits were already burned on the first call).
//
// Invariant: either both the quota update and the event insert commit, or neither does.
func RecordResourceBillingTx(tx *sql.Tx, e domain.ResourceUsageEvent, quotaBurnCredits int64, quotaUpdate func(*sql.Tx, string, int64) (int64, error)) (string, error) {
	if e.UserID == "" {
		return "", errors.New("resource billing: missing user_id")
	}
	if e.IdempotencyKey != "" {
		var exists int
		err := tx.QueryRow(
			`SELECT 1 FROM resource_usage_events WHERE idempotency_key = ? LIMIT 1`,
			e.IdempotencyKey,
		).Scan(&exists)
		if err == nil {
			return "", ErrResourceIdempotencyConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}

	// Quota burn first: actualBurn is what actually came out of the user's balance.
	// For admin/enforcement-disabled paths this is 0 even when quotaBurnCredits > 0.
	actualBurn := int64(0)
	if e.Success && quotaBurnCredits > 0 && quotaUpdate != nil {
		var err error
		actualBurn, err = quotaUpdate(tx, e.UserID, quotaBurnCredits)
		if err != nil {
			return "", err
		}
	}

	// Store loaded cost only when credits were actually burned.
	loadedMicro := int64(0)
	if actualBurn > 0 {
		loadedMicro = e.LoadedCostUSDMicro
	}

	success := 0
	if e.Success {
		success = 1
	}
	_, err := tx.Exec(`
		INSERT INTO resource_usage_events (
			id, user_id, resource_type, operation_id, idempotency_key,
			automation_run_id, application_id, job_id,
			quantity, unit, duration_ms,
			raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned,
			success, error_code, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`,
		e.ID, e.UserID, string(e.ResourceType), e.OperationID, e.IdempotencyKey,
		e.AutomationRunID, e.ApplicationID, e.JobID,
		e.Quantity, e.Unit, e.DurationMS,
		e.RawCostUSDMicro, loadedMicro, actualBurn,
		success, e.ErrorCode, resourceMetadataOrEmpty(e.MetadataJSON),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrResourceIdempotencyConflict
		}
		return "", err
	}
	return e.ID, nil
}

func resourceMetadataOrEmpty(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}
