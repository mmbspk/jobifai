package llmpolicy_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func TestStore_ApprovedPolicyChangesRuntimeModel(t *testing.T) {
	t.Parallel()
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	_, err = sqldb.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens)
		VALUES (?, 'approved', 'claude', 'claude-haiku-4-5', '[]', 'default', 4096)`,
		domain.TaskJobScoring,
	)
	require.NoError(t, err)

	store := &llmpolicy.Store{DB: sqldb}
	pol, err := store.ApprovedPolicy(domain.TaskJobScoring)
	require.NoError(t, err)
	require.NotNil(t, pol)

	global := domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}
	base := llm.New(global, "key")
	out, err := llmpolicy.ApplyTask(base, global, map[string]domain.TaskModel{}, pol, pricing.DefaultCatalog(), "scoring")
	require.NoError(t, err)
	res, err := llmpolicy.ResolveTaskModel("scoring", global, nil, pol, pricing.DefaultCatalog(), "claude")
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", res.Model)
	_ = out
}
