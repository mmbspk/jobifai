package engine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/policy"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func TestEvalE2E_FakeProviderRecommendApproveRollback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	_, err = sqldb.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens)
		VALUES (?, 'approved', 'claude', 'claude-sonnet-4-6', '[]', 'pinned', 8192)`,
		domain.TaskJobScoring)
	require.NoError(t, err)

	svc := &engine.Service{
		DB:  sqldb,
		Run: engine.FakeRunner{},
		Config: engine.Config{MaxBudgetMicro: 5_000_000, MaxConcurrency: 2, MinCases: 3},
	}
	ctx := context.Background()
	id, err := svc.CreateRun(ctx, domain.TaskJobScoring, "smoke",
		candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		[]candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		1.0, "admin-test")
	require.NoError(t, err)

	deadline := time.Now().Add(15 * time.Second)
	for {
		var st string
		_ = sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&st)
		if st == "completed" || st == "failed" || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	var status string
	require.NoError(t, sqldb.QueryRow(`SELECT status FROM model_eval_runs WHERE id=?`, id).Scan(&status))
	require.Equal(t, "completed", status)

	var recCount int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM model_eval_recommendations WHERE eval_run_id=?`, id).Scan(&recCount))
	assert.GreaterOrEqual(t, recCount, 1)

	approver := &policy.Approver{DB: sqldb, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude"}
	err = approver.ApproveSameProvider(ctx, domain.TaskJobScoring, id, "admin",
		candidate.Spec{Provider: "claude", Model: "claude-haiku-4-5-20251001"})
	require.NoError(t, err)

	store := &llmpolicy.Store{DB: sqldb}
	pol, err := store.ApprovedPolicy(domain.TaskJobScoring)
	require.NoError(t, err)
	require.NotNil(t, pol)
	assert.Equal(t, "claude-haiku-4-5-20251001", pol.Model)

	require.NoError(t, approver.Rollback(ctx, domain.TaskJobScoring, "admin"))
}
