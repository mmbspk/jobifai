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

func TestCreateRun_PersistsBaselineSpecJSON(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	baseline := candidate.Spec{
		Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium",
		MaxTokens: 8192, TimeoutSec: 120,
	}
	sync := false
	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 1}}
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskJobScoring, DatasetVersion: "smoke", DatasetSource: "synthetic",
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerFake,
		Baseline: baseline, Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD: 1, InitiatedBy: "test", StartAsync: &sync,
	})
	require.NoError(t, err)
	require.NoError(t, svc.ExecuteRun(context.Background(), id))

	var stored string
	require.NoError(t, sqldb.QueryRow(`SELECT baseline_spec_json FROM model_eval_runs WHERE id=?`, id).Scan(&stored))
	require.Contains(t, stored, `"timeout_sec":120`)
	require.Contains(t, stored, `"max_tokens":8192`)

	var count int
	require.NoError(t, sqldb.QueryRow(`
		SELECT COUNT(*) FROM model_eval_results
		WHERE eval_run_id=? AND provider=? AND requested_model=? AND candidate_max_tokens=? AND candidate_timeout_sec=?`,
		id, baseline.Provider, baseline.Model, baseline.MaxTokens, baseline.TimeoutSec).Scan(&count))
	require.Greater(t, count, 0)
}
