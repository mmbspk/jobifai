package llmpolicy

import (
	"database/sql"
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
)

// Store loads task model policies from SQLite.
type Store struct {
	DB *sql.DB
}

// ApprovedPolicy returns the approved policy row for a stable task, or nil.
func (s *Store) ApprovedPolicy(task string) (*domain.TaskModelPolicyRow, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	stable := domain.LegacyToStableTask(task)
	var state, provider, model, fallback, mode, effort string
	var maxTok int
	var maxCost float64
	var timeoutSec int
	var approvedBy, evalID sql.NullString
	err := s.DB.QueryRow(`
		SELECT state, provider, model, fallback_models, mode, max_tokens,
		       COALESCE(effort,''), COALESCE(max_cost_usd,0), COALESCE(timeout_sec,0),
		       approved_by, eval_run_id
		FROM task_model_policies WHERE task = ?`, stable).Scan(
		&state, &provider, &model, &fallback, &mode, &maxTok,
		&effort, &maxCost, &timeoutSec, &approvedBy, &evalID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if domain.TaskModelPolicyState(state) != domain.PolicyStateApproved || model == "" {
		return nil, nil
	}
	row := &domain.TaskModelPolicyRow{
		Task:       stable,
		State:      domain.PolicyStateApproved,
		Provider:   provider,
		Model:      model,
		Mode:       mode,
		MaxTokens:  maxTok,
		Effort:     effort,
		MaxCostUSD: maxCost,
		TimeoutSec: timeoutSec,
	}
	if approvedBy.Valid {
		row.ApprovedBy = approvedBy.String
	}
	if evalID.Valid {
		row.EvalRunID = evalID.String
	}
	_ = json.Unmarshal([]byte(fallback), &row.FallbackModels)
	return row, nil
}
