package db

import (
	"database/sql"

	"github.com/user/jobifai/internal/domain"
)

// ModelUsage holds cumulative token counts for a single model.
type ModelUsage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	Calls        int
}

// IncrementUsage adds delta token counts for a (user, model) pair via an UPSERT.
func IncrementUsage(db *sql.DB, userID, model string, inputTokens, outputTokens int64, calls int) error {
	_, err := db.Exec(`
		INSERT INTO usage_totals (user_id, model, input_tokens, output_tokens, calls, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, model) DO UPDATE SET
			input_tokens  = input_tokens  + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			calls         = calls         + excluded.calls,
			updated_at    = CURRENT_TIMESTAMP`,
		userID, model, inputTokens, outputTokens, calls)
	return err
}

// TotalUsageByModel returns per-model cumulative usage for a user.
func TotalUsageByModel(db *sql.DB, userID string) ([]ModelUsage, error) {
	rows, err := db.Query(`
		SELECT model, input_tokens, output_tokens, calls
		FROM usage_totals WHERE user_id = ? ORDER BY model`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.Model, &m.InputTokens, &m.OutputTokens, &m.Calls); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// TotalUsage returns aggregate cumulative usage for a user across all models.
func TotalUsage(db *sql.DB, userID string) (domain.SessionUsage, error) {
	rows, err := TotalUsageByModel(db, userID)
	if err != nil {
		return domain.SessionUsage{}, err
	}
	var u domain.SessionUsage
	for _, r := range rows {
		u.InputTokens += r.InputTokens
		u.OutputTokens += r.OutputTokens
		u.Calls += r.Calls
	}
	return u, nil
}
