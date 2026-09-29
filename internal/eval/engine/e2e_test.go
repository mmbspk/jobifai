package engine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/pricing"
)

func TestEvalE2E_FakeRunCannotApprovePolicy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 2}}
	ctx := context.Background()
	id, err := svc.CreateRun(ctx, engine.RunParams{
		Task: domain.TaskJobScoring, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerFake,
		Baseline: candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD: 1, InitiatedBy: "test",
	})
	require.NoError(t, err)
	require.NoError(t, engine.WaitForRunWithDiagnostics(sqldb, id, 60*time.Second))

	var status string
	require.NoError(t, sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&status))
	require.Equal(t, runmeta.StatusCompleted, status, engine.RunDiagnostics(sqldb, id))

	var recID string
	require.NoError(t, sqldb.QueryRow(`SELECT id FROM model_eval_recommendations WHERE eval_run_id=? LIMIT 1`, id).Scan(&recID),
		engine.RunDiagnostics(sqldb, id))
	approver := &policy.Approver{DB: sqldb, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	err = approver.ApproveRecommendation(ctx, domain.TaskJobScoring, recID, "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fake")
}

func TestEvalE2E_AsyncExecuteRunCompletes(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 2}}
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskFormAnswer, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeSmoke, RunnerType: runmeta.RunnerFake,
		Baseline:   candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD:  1, InitiatedBy: "test",
	})
	require.NoError(t, err)
	require.NoError(t, engine.WaitForRunWithDiagnostics(sqldb, id, 60*time.Second))
	var completed, planned int
	require.NoError(t, sqldb.QueryRow(`SELECT cases_completed, cases_planned FROM model_eval_runs WHERE id=?`, id).Scan(&completed, &planned))
	require.Equal(t, planned, completed)
}
