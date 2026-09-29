package engine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/runmeta"
)

func TestExecuteRun_BudgetExhaustedRecommendationsNotDeployable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 1}}
	sync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskJobScoring, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerReal,
		Baseline:   candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD:  0.000001,
		InitiatedBy: "test",
		StartAsync: &sync,
	})
	require.NoError(t, err)
	_, _ = sqldb.Exec(`UPDATE model_eval_runs SET max_budget_usd_micro=1 WHERE id=?`, id)
	require.NoError(t, svc.ExecuteRun(context.Background(), id))

	var status string
	var deploy int
	require.NoError(t, sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&status))
	require.NotEqual(t, runmeta.StatusCompleted, status)
	require.NoError(t, sqldb.QueryRow(`SELECT deployable FROM model_eval_recommendations WHERE eval_run_id=? LIMIT 1`, id).Scan(&deploy))
	require.Equal(t, 0, deploy)
}
