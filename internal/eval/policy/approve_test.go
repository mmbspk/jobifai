package policy_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/pricing"
)

func TestApprover_RollbackWritesCorrectAudit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prev, _ := json.Marshal(domain.TaskModelPolicyRow{
		Task: domain.TaskJobScoring, Model: "claude-sonnet-4-6", Provider: "claude", State: domain.PolicyStateApproved,
	})
	_, err = db.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, previous_json)
		VALUES ('job_scoring','approved','claude','claude-haiku-4-5-20251001','[]','pinned',4096,?)`, string(prev))
	require.NoError(t, err)

	a := &policy.Approver{DB: db}
	require.NoError(t, a.Rollback(context.Background(), domain.TaskJobScoring, "admin"))

	var model string
	require.NoError(t, db.QueryRow(`SELECT model FROM task_model_policies WHERE task='job_scoring'`).Scan(&model))
	require.Equal(t, "claude-sonnet-4-6", model)

	var prevJSON, newJSON string
	require.NoError(t, db.QueryRow(`
		SELECT previous_json, new_json FROM model_policy_audit WHERE task='job_scoring' ORDER BY created_at DESC LIMIT 1`).
		Scan(&prevJSON, &newJSON))
	require.Contains(t, prevJSON, "claude-haiku")
	require.Contains(t, newJSON, "claude-sonnet-4-6")
}

var _ = pricing.DefaultCatalog
