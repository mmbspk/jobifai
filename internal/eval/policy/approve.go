package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/recommend"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
)

// Approver validates and writes approved task policies with audit trail.
type Approver struct {
	DB           *sql.DB
	Catalog      *pricing.Catalog
	BaseProvider string
}

// ApproveRecommendation promotes the candidate tied to a persisted recommendation row.
func (a *Approver) ApproveRecommendation(ctx context.Context, task, recommendationID, adminID string) error {
	rec, run, err := a.loadRecommendation(ctx, recommendationID)
	if err != nil {
		return err
	}
	if rec.Task != task {
		return fmt.Errorf("recommendation task mismatch")
	}
	if run.RunnerType != runmeta.RunnerReal {
		return fmt.Errorf("cannot approve policy from %s eval run", run.RunnerType)
	}
	if run.Purpose != runmeta.PurposeBenchmark {
		return fmt.Errorf("only benchmark eval runs can approve production policy")
	}
	if run.Status != runmeta.StatusCompleted {
		return fmt.Errorf("eval run not completed (status=%s)", run.Status)
	}
	if rec.Deployable != 1 || rec.Outcome != recommend.OutcomeRecommend {
		return fmt.Errorf("recommendation not deployable: %s", rec.Outcome)
	}
	var cand candidate.Spec
	if err := json.Unmarshal([]byte(rec.CandidateJSON), &cand); err != nil {
		return err
	}
	if a.BaseProvider != "" && cand.Provider != a.BaseProvider {
		return fmt.Errorf("cross-provider production routing not implemented")
	}
	if cand.Effort != "" && !llm.EffortSupported(cand.Provider, cand.Model, cand.Effort) {
		return fmt.Errorf("effort %q unsupported for %s/%s", cand.Effort, cand.Provider, cand.Model)
	}
	if task == domain.TaskFormVision && !llm.ModelSupportsVision(cand.Provider, cand.Model) {
		return fmt.Errorf("model does not support vision")
	}
	if a.Catalog != nil {
		if _, err := a.Catalog.Resolve(cand.Model, true); err != nil {
			return fmt.Errorf("model not in approved billing catalog: %w", err)
		}
	}
	var resultCount int
	if err := a.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM model_eval_results
		WHERE eval_run_id=? AND provider=? AND requested_model=? AND COALESCE(effort,'')=COALESCE(?,'')
		  AND candidate_max_tokens=? AND candidate_timeout_sec=?`,
		run.ID, cand.Provider, cand.Model, cand.Effort, cand.MaxTokens, cand.TimeoutSec).Scan(&resultCount); err != nil {
		return err
	}
	if resultCount == 0 {
		return fmt.Errorf("candidate has no persisted eval results")
	}
	return a.applyPolicyTx(ctx, task, run.ID, adminID, cand)
}

type recRow struct {
	ID, Task, Outcome, CandidateJSON string
	Deployable                       int
}

type runMeta struct {
	ID, Status, RunnerType, Purpose, DatasetHash string
}

func (a *Approver) loadRecommendation(ctx context.Context, id string) (recRow, runMeta, error) {
	var rec recRow
	var runID string
	err := a.DB.QueryRowContext(ctx, `
		SELECT id, task, outcome, candidate_json, deployable, eval_run_id
		FROM model_eval_recommendations WHERE id=?`, id).Scan(
		&rec.ID, &rec.Task, &rec.Outcome, &rec.CandidateJSON, &rec.Deployable, &runID)
	if err != nil {
		return recRow{}, runMeta{}, fmt.Errorf("recommendation not found")
	}
	var run runMeta
	run.ID = runID
	err = a.DB.QueryRowContext(ctx, `
		SELECT status, COALESCE(runner_type,'fake'), COALESCE(run_purpose,'smoke'), COALESCE(dataset_hash,'')
		FROM model_eval_runs WHERE id=?`, runID).Scan(&run.Status, &run.RunnerType, &run.Purpose, &run.DatasetHash)
	return rec, run, err
}

func (a *Approver) applyPolicyTx(ctx context.Context, task, evalRunID, adminID string, cand candidate.Spec) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var curProvider, curModel, curMode, curEffort string
	var curMax, curTimeout int
	var curMaxCost float64
	var prevJSON sql.NullString
	_ = tx.QueryRowContext(ctx, `
		SELECT COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(max_tokens,0),
		       COALESCE(effort,''), COALESCE(timeout_sec,0), COALESCE(max_cost_usd,0), COALESCE(previous_json,'')
		FROM task_model_policies WHERE task=?`, task).Scan(
		&curProvider, &curModel, &curMode, &curMax, &curEffort, &curTimeout, &curMaxCost, &prevJSON)

	prev := prevJSON.String
	if curModel != "" {
		b, _ := json.Marshal(domain.TaskModelPolicyRow{
			Task: task, State: domain.PolicyStateApproved,
			Provider: curProvider, Model: curModel, Mode: curMode, MaxTokens: curMax, Effort: curEffort,
			TimeoutSec: curTimeout, MaxCostUSD: curMaxCost,
		})
		prev = string(b)
	}
	maxCost := curMaxCost
	if curModel == "" {
		maxCost = 0
	}
	newPol := domain.TaskModelPolicyRow{
		Task: task, State: domain.PolicyStateApproved,
		Provider: cand.Provider, Model: cand.Model, Effort: cand.Effort,
		MaxTokens: cand.MaxTokens, TimeoutSec: cand.TimeoutSec, MaxCostUSD: maxCost,
		Mode: "pinned", EvalRunID: evalRunID,
	}
	newJSON, _ := json.Marshal(newPol)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, effort, timeout_sec, max_cost_usd, approved_by, eval_run_id, previous_json, updated_at)
		VALUES (?, 'approved', ?, ?, '[]', 'pinned', ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task) DO UPDATE SET
			state='approved', provider=excluded.provider, model=excluded.model,
			max_tokens=excluded.max_tokens, effort=excluded.effort, timeout_sec=excluded.timeout_sec,
			max_cost_usd=excluded.max_cost_usd,
			approved_by=excluded.approved_by, eval_run_id=excluded.eval_run_id,
			previous_json=excluded.previous_json, updated_at=CURRENT_TIMESTAMP`,
		task, cand.Provider, cand.Model, cand.MaxTokens, cand.Effort, cand.TimeoutSec, maxCost, adminID, evalRunID, prev,
	)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO model_policy_audit (id, task, changed_by, previous_json, new_json, eval_run_id, reason)
		VALUES (?,?,?,?,?,?,?)`,
		uuid.NewString(), task, adminID, prev, string(newJSON), evalRunID, "approve eval recommendation",
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Rollback restores previous_json from task_model_policies atomically.
func (a *Approver) Rollback(ctx context.Context, task, adminID string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var curProvider, curModel, curMode, curEffort string
	var curMax, curTimeout int
	var curMaxCost float64
	var prevJSON sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(max_tokens,0),
		       COALESCE(effort,''), COALESCE(timeout_sec,0), COALESCE(max_cost_usd,0), previous_json
		FROM task_model_policies WHERE task=?`, task).Scan(
		&curProvider, &curModel, &curMode, &curMax, &curEffort, &curTimeout, &curMaxCost, &prevJSON)
	if err != nil || !prevJSON.Valid || prevJSON.String == "" {
		return fmt.Errorf("no previous policy to restore")
	}
	currentJSON, _ := json.Marshal(domain.TaskModelPolicyRow{
		Task: task, State: domain.PolicyStateApproved,
		Provider: curProvider, Model: curModel, Mode: curMode, MaxTokens: curMax, Effort: curEffort,
		TimeoutSec: curTimeout, MaxCostUSD: curMaxCost,
	})
	var restore domain.TaskModelPolicyRow
	if err := json.Unmarshal([]byte(prevJSON.String), &restore); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE task_model_policies SET state='approved', provider=?, model=?, mode=?, max_tokens=?, effort=?,
			timeout_sec=?, max_cost_usd=?, previous_json=NULL, updated_at=CURRENT_TIMESTAMP WHERE task=?`,
		restore.Provider, restore.Model, restore.Mode, restore.MaxTokens, restore.Effort,
		restore.TimeoutSec, restore.MaxCostUSD, task,
	)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO model_policy_audit (id, task, changed_by, previous_json, new_json, reason)
		VALUES (?,?,?,?,?,?)`,
		uuid.NewString(), task, adminID, string(currentJSON), prevJSON.String, "rollback",
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}
