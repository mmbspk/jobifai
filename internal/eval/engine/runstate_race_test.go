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

func TestExecuteRun_CancelBeforeStartPersistsTerminalStatus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 2}}
	startAsync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskFormAnswer, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeSmoke, RunnerType: runmeta.RunnerFake,
		Baseline:   candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD:  1, InitiatedBy: "test",
		StartAsync: &startAsync,
	})
	require.NoError(t, err)

	execCtx, execCancel := context.WithCancel(context.Background())
	engine.Runs.Register(id, execCancel)
	t.Cleanup(func() { engine.Runs.Unregister(id); execCancel() })

	require.NoError(t, svc.CancelRun(context.Background(), id))
	execCancel()
	_ = svc.ExecuteRun(execCtx, id)

	var status string
	require.NoError(t, sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&status))
	require.Contains(t, []string{
		runmeta.StatusCancelled, runmeta.StatusBudgetExhausted,
		runmeta.StatusCompleted, runmeta.StatusFailed,
	}, status, engine.RunDiagnostics(sqldb, id))
}
