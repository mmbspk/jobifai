package bot

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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

	gs := config.ResolveOperationalSettings(cfgStore, userID, false)
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

	gs := config.ResolveOperationalSettings(cfgStore, userID, false)
	_, _, _, _, err = m.buildPerUserLLM(userID, gs)
	require.Error(t, err)
}

// TestUserLLMClient_TesterBilledAsPersonal verifies that a tester using their personal
// key produces a client flagged with personal_provider=true.
func TestUserLLMClient_TesterBilledAsPersonal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, "test-key")
	userID := "tester-user"

	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-personal"))

	m := NewManager(t.Context(), sqldb, cfgStore, secrets, nil, nil, nil, nil, nil, "resume_markets")
	// Mark user as tester so personal key is allowed.
	m.SetUserTypeChecker(func(uid string) (bool, bool) {
		return false, uid == userID // is_tester = true for this user
	})

	gs := config.ResolveOperationalSettings(cfgStore, userID, true)
	gs.LLM.Provider = "claude"
	gs.LLM.Model = "claude-haiku-4-5"

	client := m.userLLMClient(userID, gs)
	require.NotNil(t, client, "tester with personal key must get a client")
	assert.True(t, client.IsPersonalProvider(), "tester client must be flagged as personal provider")
}

// TestUserLLMClient_RegularUserBilledAsShared verifies that a regular user is never
// given personal-provider attribution even when a personal key exists in the DB.
func TestUserLLMClient_RegularUserBilledAsShared(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, "test-key")
	userID := "regular-user"

	require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "sk-system"))
	// Plant a legacy personal key — must not be used after role enforcement.
	require.NoError(t, secrets.Set(userID, "llm_api_key", "sk-legacy-personal"))

	m := NewManager(t.Context(), sqldb, cfgStore, secrets, nil, nil, nil, nil, nil, "resume_markets")
	m.SetUserTypeChecker(func(_ string) (bool, bool) { return false, false })

	gs := config.ResolveOperationalSettings(cfgStore, userID, false)
	gs.LLM.Provider = "claude"
	gs.LLM.Model = "claude-haiku-4-5"

	client := m.userLLMClient(userID, gs)
	require.NotNil(t, client, "regular user must still get a client (via system key)")
	assert.False(t, client.IsPersonalProvider(), "regular user must not be flagged as personal provider")
}
