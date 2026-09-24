package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

// Bot field is nil in these tests; the handlers gracefully degrade.

func TestBot_Status_NilBot_ReturnsIdle(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstatus@example.com", "password123")

	w := authGet(t, router, "/api/bot/status", token)
	assert.Equal(t, 200, w.Code)

	var status domain.BotStatus
	require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestBot_Start_NilBot_Returns503(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstart@example.com", "password123")

	w := authPost(t, router, "/api/bot/start", token, map[string]string{"platform": "linkedin"})
	assert.Equal(t, 503, w.Code)
}

func TestBot_Stop_NilBot_Returns503(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstop@example.com", "password123")

	w := authPost(t, router, "/api/bot/stop", token, nil)
	assert.Equal(t, 503, w.Code)
}

func TestBot_Status_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/bot/status", "")
	assert.Equal(t, 401, w.Code)
}

func TestBot_Start_WithStubController_ReturnsRunning(t *testing.T) {
	svc, _ := newTestServices(t)
	rec := &recordingBot{state: domain.BotStateIdle}
	svc.Bot = rec
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "bot-live@example.com", "password123")

	wStart := authPost(t, router, "/api/bot/start", token, map[string]string{"platform": "seek"})
	assert.Equal(t, 200, wStart.Code)
	require.Equal(t, []domain.Platform{domain.PlatformSeek}, rec.started)

	wStatus := authGet(t, router, "/api/bot/status", token)
	var status domain.BotStatus
	require.NoError(t, json.NewDecoder(wStatus.Body).Decode(&status))
	assert.Equal(t, domain.BotStateRunning, status.State)
}

func TestBot_PauseResume_WithStubController(t *testing.T) {
	svc, _ := newTestServices(t)
	rec := &recordingBot{state: domain.BotStateRunning}
	svc.Bot = rec
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "bot-pause@example.com", "password123")

	wPause := authPost(t, router, "/api/bot/pause", token, nil)
	assert.Equal(t, 200, wPause.Code)

	wStatus := authGet(t, router, "/api/bot/status", token)
	var paused domain.BotStatus
	require.NoError(t, json.NewDecoder(wStatus.Body).Decode(&paused))
	assert.Equal(t, domain.BotStatePaused, paused.State)

	wResume := authPost(t, router, "/api/bot/resume", token, nil)
	assert.Equal(t, 200, wResume.Code)

	wStatus2 := authGet(t, router, "/api/bot/status", token)
	var running domain.BotStatus
	require.NoError(t, json.NewDecoder(wStatus2.Body).Decode(&running))
	assert.Equal(t, domain.BotStateRunning, running.State)
}

func TestBot_Start_TestAutomation_MockLLM_WritesSkippedJob(t *testing.T) {
	scoreBody := `{"score": 8, "reasoning": "HTTP integration path."}`
	srv := mockllm.ClaudeServer(t, scoreBody)
	t.Cleanup(srv.Close)

	svc, db := newTestServices(t)
	sessions := browser.NewSessionStore(db, svc.Secrets)
	svc.SessionStore = sessions

	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "bot-http@example.com", "password123")
	userID := userIDFromToken(t, router, token)

	wireMockLLMUser(t, svc, userID, srv.URL)
	saveResumeProfile(t, router, token, sampleResumeProfile())

	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "session", Value: "mock", Domain: ".example.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(bot.PlatformTestAutomation), string(domain.LoginMethodManual), raw))

	scorer := resume.NewScorer(mockllm.NewClaudeClient(t, srv))
	renderer := resume.NewPDFRenderer("resume_style")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr := bot.NewManager(ctx, db, svc.Config, svc.Secrets, sessions, nil, scorer, nil, renderer, "")
	svc.Bot = mgr
	router = handler.NewRouter(svc)

	wStart := authPost(t, router, "/api/bot/start", token, map[string]string{"platform": string(bot.PlatformTestAutomation)})
	require.Equal(t, http.StatusOK, wStart.Code)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id = ?`, userID).Scan(&n); err == nil && n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	wSkipped := authGet(t, router, "/api/jobs/skipped", token)
	var skipped []domain.SkippedJob
	require.NoError(t, json.NewDecoder(wSkipped.Body).Decode(&skipped))
	require.NotEmpty(t, skipped)
	assert.Equal(t, bot.PlatformTestAutomation, skipped[0].Platform)
}
