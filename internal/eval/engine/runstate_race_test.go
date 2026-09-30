package engine_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/runmeta"
)

func TestExecuteRun_BudgetAndCancelRaceSafe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 4}}
	startAsync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskFormAnswer, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerReal,
		Baseline:   candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD:  0.000001, InitiatedBy: "test",
		StartAsync: &startAsync,
	})
	require.NoError(t, err)
	_, _ = sqldb.Exec(`UPDATE model_eval_runs SET max_budget_usd_micro=1 WHERE id=?`, id)

	execCtx, execCancel := context.WithCancel(context.Background())
	engine.Runs.Register(id, execCancel)
	t.Cleanup(func() { engine.Runs.Unregister(id); execCancel() })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.ExecuteRun(execCtx, id)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(2 * time.Millisecond)
		_ = svc.CancelRun(context.Background(), id)
	}()
	wg.Wait()

	var status string
	require.NoError(t, sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&status))
	require.Contains(t, []string{
		runmeta.StatusBudgetExhausted, runmeta.StatusCancelled,
		runmeta.StatusCompleted, runmeta.StatusFailed,
	}, status)
}
