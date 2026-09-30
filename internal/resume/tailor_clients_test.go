package resume_test

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
	"github.com/user/jobifai/internal/resume"
)

func TestTailor_FormAnswerAndVisionClientsResolveIndependently(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	_, err = sqldb.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens)
		VALUES ('form_vision', 'approved', 'claude', 'claude-sonnet-4-5-20250929', '[]', 'pinned', 4096)`)
	require.NoError(t, err)

	policyStore := &llmpolicy.Store{DB: sqldb}
	catalog := pricing.DefaultCatalog()
	gs := domain.GeneralSettings{
		LLM: domain.LLMConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-6",
			TaskModels: map[string]domain.TaskModel{
				"form_filling": {Model: "claude-haiku-4-5-20251001"},
			},
		},
	}
	base := llm.New(gs.LLM, "test-key")
	formAnswerC, err := llmpolicy.ApplyTask(base, gs.LLM, gs.LLM.TaskModels, mustPolicy(t, policyStore, "form_filling"), catalog, "form_filling")
	require.NoError(t, err)
	formVisionC, err := llmpolicy.ApplyTask(base, gs.LLM, gs.LLM.TaskModels, mustPolicy(t, policyStore, domain.TaskFormVision), catalog, domain.TaskFormVision)
	require.NoError(t, err)

	tailor := resume.NewTailor(base, base, formAnswerC, formVisionC)
	assert.Equal(t, "claude-haiku-4-5-20251001", tailor.FormAnswerClient().ModelName())
	assert.Equal(t, "claude-sonnet-4-5-20250929", tailor.FormVisionClient().ModelName())
}

func mustPolicy(t *testing.T, s *llmpolicy.Store, task string) *domain.TaskModelPolicyRow {
	t.Helper()
	p, err := s.ApprovedPolicy(task)
	require.NoError(t, err)
	return p
}
