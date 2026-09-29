package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/pricing"
)

// Approver validates and writes approved task policies with audit trail.
type Approver struct {
	DB      *sql.DB
	Catalog *pricing.Catalog
	BaseProvider string
}

// ApproveSameProvider promotes a candidate from a completed eval run.
func (a *Approver) ApproveSameProvider(ctx context.Context, task, evalRunID, adminID string, cand candidate.Spec) error {
	if a.BaseProvider != "" && cand.Provider != a.BaseProvider {
		return fmt.Errorf("cross-provider production routing not implemented")
	}
	if a.Catalog != nil {
		if _, err := a.Catalog.Resolve(cand.Model, true); err != nil {
			return fmt.Errorf("model not in approved billing catalog: %w", err)
		}
	}
	var status string
	err := a.DB.QueryRowContext(ctx, `SELECT status FROM model_eval_runs WHERE id=?`, evalRunID).Scan(&status)
	if err != nil {
		return err
	}
	if status != "completed" {
		return fmt.Errorf("eval run not completed")
	}
	var prev json.RawMessage
	var curProvider, curModel, curMode string
	var curMax int
	var curEffort string
	err = a.DB.QueryRowContext(ctx, `
		SELECT COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(max_tokens,0), COALESCE(effort,'')
		FROM task_model_policies WHERE task=?`, task).Scan(&curProvider, &curModel, &curMode, &curMax, &curEffort)
	if err == nil && curModel != "" {
		prev, _ = json.Marshal(domain.TaskModelPolicyRow{
			Task: task, Provider: curProvider, Model: curModel, Mode: curMode, MaxTokens: curMax, Effort: curEffort,
			State: domain.PolicyStateApproved,
		})
	}
	newPol := domain.TaskModelPolicyRow{
		Task: task, State: domain.PolicyStateApproved,
		Provider: cand.Provider, Model: cand.Model, Effort: cand.Effort,
		MaxTokens: cand.MaxTokens, Mode: "pinned", EvalRunID: evalRunID,
	}
	newJSON, _ := json.Marshal(newPol)
	_, err = a.DB.ExecContext(ctx, `
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, effort, approved_by, eval_run_id, previous_json, updated_at)
		VALUES (?, 'approved', ?, ?, '[]', 'pinned', ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task) DO UPDATE SET
			state='approved', provider=excluded.provider, model=excluded.model,
			max_tokens=excluded.max_tokens, effort=excluded.effort,
			approved_by=excluded.approved_by, eval_run_id=excluded.eval_run_id,
			previous_json=excluded.previous_json, updated_at=CURRENT_TIMESTAMP`,
		task, cand.Provider, cand.Model, cand.MaxTokens, cand.Effort, adminID, evalRunID, string(prev),
	)
	if err == nil && len(prev) > 0 {
		_, _ = a.DB.ExecContext(ctx, `UPDATE task_model_policies SET previous_json=? WHERE task=?`, string(prev), task)
	}
	if err != nil {
		return err
	}
	_, err = a.DB.ExecContext(ctx, `
		INSERT INTO model_policy_audit (id, task, changed_by, previous_json, new_json, eval_run_id, reason)
		VALUES (?,?,?,?,?,?,?)`,
		uuid.NewString(), task, adminID, string(prev), string(newJSON), evalRunID, "approve eval recommendation",
	)
	return err
}

// Rollback restores previous_json from task_model_policies.
func (a *Approver) Rollback(ctx context.Context, task, adminID string) error {
	var prev sql.NullString
	err := a.DB.QueryRowContext(ctx, `SELECT previous_json FROM task_model_policies WHERE task=?`, task).Scan(&prev)
	if err != nil || !prev.Valid || prev.String == "" {
		return fmt.Errorf("no previous policy to restore")
	}
	var pol domain.TaskModelPolicyRow
	if err := json.Unmarshal([]byte(prev.String), &pol); err != nil {
		return err
	}
	cur, _ := json.Marshal(pol)
	_, err = a.DB.ExecContext(ctx, `
		UPDATE task_model_policies SET state='approved', provider=?, model=?, max_tokens=?, effort=?,
			previous_json=NULL, updated_at=CURRENT_TIMESTAMP WHERE task=?`,
		pol.Provider, pol.Model, pol.MaxTokens, pol.Effort, task,
	)
	if err != nil {
		return err
	}
	_, err = a.DB.ExecContext(ctx, `
		INSERT INTO model_policy_audit (id, task, changed_by, previous_json, new_json, reason)
		VALUES (?,?,?,?,?,?)`,
		uuid.NewString(), task, adminID, string(cur), prev.String, "rollback",
	)
	return err
}
