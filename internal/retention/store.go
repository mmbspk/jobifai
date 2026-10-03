package retention

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func insertAudit(ctx context.Context, db DB, adminUserID string, prev, next int) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO document_retention_audit (id, admin_user_id, previous_limit, new_limit) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), adminUserID, prev, next)
	return err
}

type AuditRow struct {
	ID            string `json:"id"`
	AdminUserID   string `json:"admin_user_id"`
	PreviousLimit int    `json:"previous_limit"`
	NewLimit      int    `json:"new_limit"`
	CreatedAt     string `json:"created_at"`
}

func ListAudit(ctx context.Context, db DB, limit int) ([]AuditRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, admin_user_id, previous_limit, new_limit, created_at
		 FROM document_retention_audit ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.AdminUserID, &r.PreviousLimit, &r.NewLimit, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
