package engine_test

import (
	"context"
	"database/sql"
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
	waitComplete(t, sqldb, id)

	var recID string
	require.NoError(t, sqldb.QueryRow(`SELECT id FROM model_eval_recommendations WHERE eval_run_id=? LIMIT 1`, id).Scan(&recID))
	approver := &policy.Approver{DB: sqldb, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	err = approver.ApproveRecommendation(ctx, domain.TaskJobScoring, recID, "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fake")
}

func waitComplete(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var st string
		_ = db.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&st)
		if st == runmeta.StatusCompleted || st == runmeta.StatusFailed || st == runmeta.StatusBudgetExhausted {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
