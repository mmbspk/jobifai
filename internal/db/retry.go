package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ExecWithRetry retries db.Exec on SQLITE_BUSY ("database is locked") with
// bounded exponential backoff. The driver's _busy_timeout doesn't cover all
// WAL-writer contention paths — e.g. when the bot persists a session refresh
// while a concurrent goroutine writes llm_usage — so writes that race the
// WAL writer can return BUSY immediately.
//
// Behaviour for non-BUSY errors and for the success path is identical to
// db.Exec, so callers can swap the call in without changing semantics.
func ExecWithRetry(db *sql.DB, query string, args ...any) (sql.Result, error) {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		res, err := db.Exec(query, args...)
		if err == nil {
			return res, nil
		}
		if !IsSQLiteBusy(err) {
			return nil, err
		}
		lastErr = err
		// 50, 100, 200, 400, 800 ms — total ~1.55s upper bound on top of the
		// driver's own _busy_timeout. WAL contention is sub-second in practice.
		time.Sleep(time.Duration(50*(1<<attempt)) * time.Millisecond)
	}
	return nil, lastErr
}

// ExecContextWithRetry is the context-aware variant of ExecWithRetry. It
// honours ctx cancellation between attempts. Behaviour is otherwise
// identical to db.ExecContext on the success and non-BUSY error paths.
func ExecContextWithRetry(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		res, err := db.ExecContext(ctx, query, args...)
		if err == nil {
			return res, nil
		}
		if !IsSQLiteBusy(err) {
			return nil, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(50*(1<<attempt)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

// IsSQLiteBusy reports whether err is a transient SQLITE_BUSY result.
// modernc.org/sqlite reports the textual code in the error string; matching
// by string is the stable path that survives driver upgrades.
func IsSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY")
}
