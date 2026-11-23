package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Some existing installs have a version-53 marker without its apply-engine
// objects. Repair forward after migration 59; never rewrite migration history
// or discard existing attempt/audit records. Goose discovers this registered Go
// migration alongside the embedded SQL migrations.
func reconcileApplyEngineSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS apply_engine_audit (
 id TEXT PRIMARY KEY,
 admin_id TEXT NOT NULL,
 old_mode TEXT NOT NULL DEFAULT '',
 new_mode TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 config_json TEXT NOT NULL DEFAULT '{}',
 created_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return fmt.Errorf("reconcile apply-engine audit: %w", err)
	}
	var columns int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('apply_attempts') WHERE lower(name)='engine'`).Scan(&columns); err != nil {
		return fmt.Errorf("inspect attempt engine column: %w", err)
	}
	if columns == 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE apply_attempts ADD COLUMN engine TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("reconcile attempt engine column: %w", err)
		}
	}
	return nil
}

// Hot rollback retains schema and records. Removing repaired audit/attempt data
// would make a downgrade destructive and recreate the broken installation.
func keepApplyEngineSchema(context.Context, *sql.Tx) error { return nil }
