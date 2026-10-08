package bot

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func TestBuildPerUserLLM_HalalUsesTaskOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, "test-key")
	userID := "user-halal-test"
	// System key is used for all non-tester users; personal keys require tester role
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "fake-key"))
	require.NoError(t, cfgStore.Set(userID, "general_settings", domain.GeneralSettings{
		HalalJobFilter: true,
		LLM: domain.LLMConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-6",
			TaskModels: map[string]domain.TaskModel{
				"halal": {Model: "claude-haiku-4-5"},
			},
		},
	}))

	m := NewManager(t.Context(), sqldb, cfgStore, secrets, nil, nil, nil, nil, nil, "resume_markets")
	m.SetLLMBilling(&llmpolicy.Store{DB: sqldb}, pricing.DefaultCatalog(), nil)

	gs := config.ResolveOperationalSettings(cfgStore, userID)
	_, _, halal, _, err := m.buildPerUserLLM(userID, gs)
	require.NoError(t, err)
	require.NotNil(t, halal)
}

func TestBuildPerUserLLM_InvalidPolicyReturnsError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	_, err = sqldb.Exec(`
		INSERT INTO task_model_policies (task, state, provider, model, fallback_models, mode, max_tokens)
		VALUES ('job_scoring', 'approved', 'claude', 'unknown-model-xyz', '[]', 'default', 4096)`)
	require.NoError(t, err)

	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, "test-key")
	userID := "user-policy-bad"
	// System key is used for all non-tester users; personal keys require tester role
	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "fake-key"))
	require.NoError(t, cfgStore.Set(userID, "general_settings", domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"},
	}))

	m := NewManager(t.Context(), sqldb, cfgStore, secrets, nil, nil, nil, nil, nil, "resume_markets")
	m.SetLLMBilling(&llmpolicy.Store{DB: sqldb}, pricing.DefaultCatalog(), nil)

	gs := config.ResolveOperationalSettings(cfgStore, userID)
	_, _, _, _, err = m.buildPerUserLLM(userID, gs)
	require.Error(t, err)
}
