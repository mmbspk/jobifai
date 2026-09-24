package bot_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

func seedAutomationUser(t *testing.T, userID string, cfgStore *config.Store, secrets *config.SecretsStore, sessions *browser.SessionStore, llmSrvURL string) {
	t.Helper()
	require.NoError(t, cfgStore.Set(userID, "resume_profile", domain.ResumeProfile{
		Summary: "Ready for automation tests.",
		PersonalInformation: domain.PersonalInformation{
			Name:  "Taylor",
			Email: "taylor@example.com",
		},
	}))
	mockllm.StoreUserLLMConfig(t, cfgStore, secrets, userID, llmSrvURL)

	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "session", Value: "mock", Domain: ".example.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(bot.PlatformTestAutomation), string(domain.LoginMethodManual), raw))
}

func TestManager_Start_TestAutomationRunner_RecordsSkippedJob(t *testing.T) {
	scoreBody := `{"score": 9, "reasoning": "Mock LLM scoring for automation test."}`
	srv := mockllm.ClaudeServer(t, scoreBody)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)

	const userID = "automation-user-1"
	seedAutomationUser(t, userID, cfgStore, secrets, sessions, srv.URL)

	client := mockllm.NewClaudeClient(t, srv)
	scorer := resume.NewScorer(client)
	renderer := resume.NewPDFRenderer("resume_style")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, scorer, nil, renderer, "")

	require.NoError(t, mgr.Start(context.Background(), userID, bot.PlatformTestAutomation))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id = ? AND id = ?`, userID, "test-automation-job-1").Scan(&n)
		require.NoError(t, err)
		if n == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	var company, reason, platform string
	var score int
	err = db.QueryRow(
		`SELECT company, skip_reason, suitability_score, platform FROM jobs_skipped WHERE user_id = ? AND id = ?`,
		userID, "test-automation-job-1",
	).Scan(&company, &reason, &score, &platform)
	require.NoError(t, err)
	assert.Equal(t, "Mock Job Board", company)
	assert.Equal(t, "test_automation_complete", reason)
	assert.Equal(t, 9, score)
	assert.Equal(t, string(bot.PlatformTestAutomation), platform)

	status := mgr.Status(userID)
	assert.Contains(t, []domain.BotState{domain.BotStateStopped, domain.BotStateIdle}, status.State)
}
