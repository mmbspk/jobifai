package policy_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func seedBenchmarkApprove(t *testing.T, db *sql.DB, task string, cand candidate.Spec) (runID, recID string) {
	t.Helper()
	runID = "run-" + task
	recID = "rec-" + task
	cj, _ := json.Marshal(cand)
	_, err := db.Exec(`
		INSERT INTO model_eval_runs (id, task, baseline_model, candidate_models, dataset_version, status, runner_type, run_purpose, candidate_spec_json, baseline_spec_json, max_budget_usd_micro, initiated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		runID, task, "claude-sonnet-4-6", "[]", "full", "completed", "real", "benchmark", "[]", "{}", 2_000_000, "test")
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO model_eval_recommendations (id, eval_run_id, task, outcome, baseline_json, candidate_json, metrics_json, reason, deployable)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		recID, runID, task, "recommend", "{}", string(cj), "{}", "ok", 1)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO model_eval_results (id, eval_run_id, case_id, model, success, provider, requested_model, candidate_max_tokens, candidate_timeout_sec)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"res-"+task, runID, task+"-0001", cand.Model, 1, cand.Provider, cand.Model, cand.MaxTokens, cand.TimeoutSec)
	require.NoError(t, err)
	return runID, recID
}

func TestApprover_FirstPolicyApproveAndRollbackInherit(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM task_model_policies WHERE task=?`, domain.TaskJobScoring).Scan(&count))
	require.Equal(t, 0, count)

	haiku := candidate.Spec{
		Provider: "claude", Model: "claude-haiku-4-5-20251001",
		MaxTokens: 8192, TimeoutSec: 120, Effort: "",
	}
	_, recID := seedBenchmarkApprove(t, db, domain.TaskJobScoring, haiku)
	a := &policy.Approver{DB: db, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	require.NoError(t, a.ApproveRecommendation(context.Background(), domain.TaskJobScoring, recID, "admin"))

	var model string
	require.NoError(t, db.QueryRow(`SELECT model FROM task_model_policies WHERE task=?`, domain.TaskJobScoring).Scan(&model))
	require.Equal(t, haiku.Model, model)

	store := &llmpolicy.Store{DB: db}
	pol, err := store.ApprovedPolicy(domain.TaskJobScoring)
	require.NoError(t, err)
	require.NotNil(t, pol)
	res, err := llmpolicy.ResolveTaskModel(domain.TaskJobScoring, domain.LLMConfig{
		Provider: "claude", Model: "claude-sonnet-4-6", MaxTokens: 8192,
	}, nil, pol, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	require.Equal(t, haiku.Model, res.Model)
	require.Equal(t, "policy_approved", res.Source)

	var approvePrev, approveNew string
	require.NoError(t, db.QueryRow(`
		SELECT previous_json, new_json FROM model_policy_audit WHERE task=? ORDER BY created_at DESC LIMIT 1`,
		domain.TaskJobScoring).Scan(&approvePrev, &approveNew))
	require.True(t, policy.IsPreviousPolicyInherit(approvePrev))
	require.Contains(t, approveNew, haiku.Model)

	require.NoError(t, a.Rollback(context.Background(), domain.TaskJobScoring, "admin"))

	err = db.QueryRow(`SELECT model FROM task_model_policies WHERE task=?`, domain.TaskJobScoring).Scan(&model)
	require.ErrorIs(t, err, sql.ErrNoRows)

	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", MaxTokens: 8192}
	res, err = llmpolicy.ResolveTaskModel(domain.TaskJobScoring, global, nil, nil, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-6", res.Model)
	require.Equal(t, "global", res.Source)

	var rbPrev, rbNew string
	require.NoError(t, db.QueryRow(`
		SELECT previous_json, new_json FROM model_policy_audit WHERE task=? ORDER BY created_at DESC LIMIT 1`,
		domain.TaskJobScoring).Scan(&rbPrev, &rbNew))
	require.Contains(t, rbPrev, haiku.Model)
	require.True(t, policy.IsPreviousPolicyInherit(rbNew))
}

func TestApprover_ExistingPolicyApproveAndRollbackRestoresSonnet(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, effort, timeout_sec, max_cost_usd)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		domain.TaskJobScoring, "approved", "claude", "claude-sonnet-4-6", "[]", "pinned", 8192, "medium", 120, 0.35)
	require.NoError(t, err)

	haiku := candidate.Spec{
		Provider: "claude", Model: "claude-haiku-4-5-20251001",
		MaxTokens: 4096, TimeoutSec: 90,
	}
	_, recID := seedBenchmarkApprove(t, db, domain.TaskJobScoring, haiku)
	a := &policy.Approver{DB: db, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	require.NoError(t, a.ApproveRecommendation(context.Background(), domain.TaskJobScoring, recID, "admin"))

	var row domain.TaskModelPolicyRow
	require.NoError(t, db.QueryRow(`
		SELECT provider, model, mode, max_tokens, effort, timeout_sec, max_cost_usd
		FROM task_model_policies WHERE task=?`, domain.TaskJobScoring).
		Scan(&row.Provider, &row.Model, &row.Mode, &row.MaxTokens, &row.Effort, &row.TimeoutSec, &row.MaxCostUSD))
	require.Equal(t, haiku.Model, row.Model)

	require.NoError(t, a.Rollback(context.Background(), domain.TaskJobScoring, "admin"))

	require.NoError(t, db.QueryRow(`
		SELECT provider, model, mode, max_tokens, effort, timeout_sec, max_cost_usd
		FROM task_model_policies WHERE task=?`, domain.TaskJobScoring).
		Scan(&row.Provider, &row.Model, &row.Mode, &row.MaxTokens, &row.Effort, &row.TimeoutSec, &row.MaxCostUSD))
	require.Equal(t, "claude", row.Provider)
	require.Equal(t, "claude-sonnet-4-6", row.Model)
	require.Equal(t, "pinned", row.Mode)
	require.Equal(t, 8192, row.MaxTokens)
	require.Equal(t, "medium", row.Effort)
	require.Equal(t, 120, row.TimeoutSec)
	require.InDelta(t, 0.35, row.MaxCostUSD, 0.001)

	var auditCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM model_policy_audit WHERE task=?`, domain.TaskJobScoring).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
}
