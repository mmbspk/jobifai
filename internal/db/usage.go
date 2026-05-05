package db

import (
	"database/sql"

	"github.com/user/jobifai/internal/domain"
)

// IncrementUsage adds delta token counts for a user via an UPSERT.
func IncrementUsage(db *sql.DB, userID string, inputTokens, outputTokens int64, calls int) error {
	_, err := db.Exec(`
		INSERT INTO usage_totals (user_id, input_tokens, output_tokens, calls, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			input_tokens  = input_tokens  + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			calls         = calls         + excluded.calls,
			updated_at    = CURRENT_TIMESTAMP`,
		userID, inputTokens, outputTokens, calls)
	return err
}

// TotalUsage returns cumulative usage for a user. Returns zero values if no record exists.
func TotalUsage(db *sql.DB, userID string) (domain.SessionUsage, error) {
	var u domain.SessionUsage
	err := db.QueryRow(`
		SELECT input_tokens, output_tokens, calls
		FROM usage_totals WHERE user_id = ?`, userID).
		Scan(&u.InputTokens, &u.OutputTokens, &u.Calls)
	if err == sql.ErrNoRows {
		return domain.SessionUsage{}, nil
	}
	return u, err
}
