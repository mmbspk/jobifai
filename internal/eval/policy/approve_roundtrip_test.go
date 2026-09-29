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
)

func TestApprover_RollbackRestoresFullRuntimePolicy(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prev, _ := json.Marshal(domain.TaskModelPolicyRow{
		Task: domain.TaskFormAnswer, Model: "claude-sonnet-4-6", Provider: "claude",
		Mode: "pinned", MaxTokens: 8192, Effort: "medium", TimeoutSec: 120, MaxCostUSD: 0.25,
		State: domain.PolicyStateApproved,
	})
	_, err = db.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens, effort, timeout_sec, max_cost_usd, previous_json)
		VALUES ('form_answer','approved','claude','claude-haiku-4-5-20251001','[]','pinned',4096,'low',45,0.05,?)`, string(prev))
	require.NoError(t, err)

	a := &policy.Approver{DB: db}
	require.NoError(t, a.Rollback(context.Background(), domain.TaskFormAnswer, "admin"))

	var row domain.TaskModelPolicyRow
	row.Task = domain.TaskFormAnswer
	require.NoError(t, db.QueryRow(`
		SELECT provider, model, mode, max_tokens, effort, timeout_sec, max_cost_usd
		FROM task_model_policies WHERE task='form_answer'`).
		Scan(&row.Provider, &row.Model, &row.Mode, &row.MaxTokens, &row.Effort, &row.TimeoutSec, &row.MaxCostUSD))
	require.Equal(t, "claude-sonnet-4-6", row.Model)
	require.Equal(t, "medium", row.Effort)
	require.Equal(t, 8192, row.MaxTokens)
	require.Equal(t, 120, row.TimeoutSec)
	require.InDelta(t, 0.25, row.MaxCostUSD, 0.001)
}

func TestApplyPolicySnapshot_IncludesTimeout(t *testing.T) {
	t.Parallel()
	c := candidate.Spec{Provider: "claude", Model: "m", MaxTokens: 2048, TimeoutSec: 90, Effort: "high"}
	b, _ := json.Marshal(c)
	var back candidate.Spec
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, 90, back.TimeoutSec)
}
