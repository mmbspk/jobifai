// Package eval provides offline Jobifai task model evaluation (admin-only).
package eval

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
)

// Runner executes evaluation runs against local datasets (outside git).
type Runner struct {
	DB *sql.DB
}

// RunRequest describes a comparative evaluation.
type RunRequest struct {
	Task            string
	BaselineModel   string
	CandidateModels []string
	DatasetVersion  string
}

// Run creates a model_eval_runs row and returns its ID. Execution is async/batch in future work.
func (r *Runner) Run(ctx context.Context, req RunRequest) (string, error) {
	id := uuid.NewString()
	_, err := r.DB.ExecContext(ctx, `
		INSERT INTO model_eval_runs (id, task, baseline_model, candidate_models, dataset_version, status)
		VALUES (?, ?, ?, ?, ?, 'pending')`,
		id, req.Task, req.BaselineModel, mustJSON(req.CandidateModels), req.DatasetVersion,
	)
	return id, err
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
