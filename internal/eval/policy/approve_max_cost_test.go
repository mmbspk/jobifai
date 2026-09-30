package policy_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/pricing"
)

func TestApprover_PreservesMaxCostUSDInDBAndAudit(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, max_cost_usd, timeout_sec)
		VALUES ('form_answer','approved','claude','claude-sonnet-4-6','[]','pinned',4096,0.42,60)`)
	require.NoError(t, err)

	runID := "run-1"
	recID := "rec-1"
	cand := candidate.Spec{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 2048, TimeoutSec: 45}
	cj, _ := json.Marshal(cand)
	_, err = db.Exec(`
		INSERT INTO model_eval_runs (id, task, baseline_model, candidate_models, dataset_version, status, runner_type, run_purpose, candidate_spec_json, baseline_spec_json, max_budget_usd_micro, initiated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		runID, domain.TaskFormAnswer, "claude-sonnet-4-6", "[]", "smoke", "completed", "real", "benchmark", "[]", "{}", 1_000_000, "test")
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO model_eval_recommendations (id, eval_run_id, task, outcome, baseline_json, candidate_json, metrics_json, reason, deployable)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		recID, runID, domain.TaskFormAnswer, "recommend", "{}", string(cj), "{}", "ok", 1)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO model_eval_results (id, eval_run_id, case_id, model, success, provider, requested_model, candidate_max_tokens, candidate_timeout_sec)
		VALUES ('r1',?,?,?,?,?,?,?,?)`,
		runID, "form_answer-0001", cand.Model, 1, cand.Provider, cand.Model, cand.MaxTokens, cand.TimeoutSec)
	require.NoError(t, err)

	a := &policy.Approver{DB: db, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	require.NoError(t, a.ApproveRecommendation(context.Background(), domain.TaskFormAnswer, recID, "admin"))

	var dbMax float64
	require.NoError(t, db.QueryRow(`SELECT max_cost_usd FROM task_model_policies WHERE task='form_answer'`).Scan(&dbMax))
	require.InDelta(t, 0.42, dbMax, 0.001)

	var newJSON string
	require.NoError(t, db.QueryRow(`
		SELECT new_json FROM model_policy_audit WHERE task='form_answer' ORDER BY created_at DESC LIMIT 1`).Scan(&newJSON))
	var row domain.TaskModelPolicyRow
	require.NoError(t, json.Unmarshal([]byte(newJSON), &row))
	require.InDelta(t, 0.42, row.MaxCostUSD, 0.001)
}
