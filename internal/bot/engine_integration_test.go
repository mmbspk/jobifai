package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

func TestManagerBuildConfig_WiresEngineForBothWorkerTypes(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		t.Run(string(platform), func(t *testing.T) {
			db := openMappingDB(t)
			store := config.NewStore(db)
			secrets := config.NewSecretsStore(db, "test-key")
			sessions := browser.NewSessionStore(db, secrets)
			const userID = "engine-pilot"
			srv := mockllm.ClaudeServer(t, "[]")
			defer srv.Close()
			mockllm.StoreUserLLMConfig(t, store, secrets, userID, srv.URL)
			require.NoError(t, store.Set(userID, "resume_profile", basicProfile()))
			require.NoError(t, store.Set(userID, "work_preferences", domain.WorkPreferences{Hybrid: true}))
			require.NoError(t, store.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{
				Mode: domain.ApplyEngineAdaptive, PilotUserIDs: []string{userID}, ExternalATSEnabled: true,
			}))
			cookies, err := browser.MarshalCookies([]browser.Cookie{{Name: "session", Value: "fixture", Domain: ".example.com", Path: "/"}})
			require.NoError(t, err)
			require.NoError(t, sessions.Save(userID, string(platform), string(domain.LoginMethodManual), cookies))
			m := NewManager(context.Background(), db, store, secrets, sessions, nil, nil, nil, nil, "")
			m.userTypeChecker = func(string) (bool, bool) { return true, false }
			batch, err := m.buildConfig(userID, platform)
			require.NoError(t, err)
			single, _, err := m.setupBot(userID, platform, "")
			require.NoError(t, err)
			for _, cfg := range []*Config{batch, &single.cfg} {
				assert.Equal(t, domain.ApplyEngineAdaptive, cfg.EngineMode)
				assert.Equal(t, store, cfg.EngineStore)
				assert.True(t, cfg.EnableExternalATS)
				require.NotNil(t, cfg.AdaptiveLLM)
				require.NotNil(t, cfg.AdaptiveMapping)
				assert.True(t, cfg.Preferences.Hybrid)
				require.NotNil(t, cfg.PreferencesLoader)
				require.True(t, cfg.PreferencesLoader().Hybrid)
			}
			require.NoError(t, store.Set(userID, "work_preferences", domain.WorkPreferences{Remote: true}))
			for _, cfg := range []*Config{batch, &single.cfg} {
				current := cfg.PreferencesLoader()
				require.NotNil(t, current)
				assert.False(t, current.Hybrid, "both setup paths must reload current settings")
				assert.True(t, current.Remote)
			}
		})
	}
}

func TestApplyAttempt_NormalRoutingSelectsAdaptive(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		t.Run(string(platform), func(t *testing.T) {
			b := newTestBot(t)
			b.cfg.Platform = platform
			ac := &AttemptContext{Engine: domain.ApplyEngineAdaptive, Platform: string(platform)}
			// Both adaptive runners reject missing declared identity before browser access.
			// Legacy runners access the browser, so this exercises the actual dispatch.
			err := b.applyAttempt(context.Background(), nil, nil, ac)
			require.ErrorIs(t, err, errLinkedInIdentity)
		})
	}
}

func TestStepExecutor_RollbackBeforeSubmitPreventsDispatch(t *testing.T) {
	db := openMappingDB(t)
	store := config.NewStore(db)
	ac := newAttemptContext("pilot", "seek", "123", "https://seek.com.au/job/123", "Coordinator", "Acme", "fixture", "run")
	ac.Engine = domain.ApplyEngineAdaptive
	beginAttempt(db, ac)
	require.NoError(t, store.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	se := NewStepExecutor(basicBounds(), &SubmitGuard{DB: db, UserID: ac.UserID, AttemptID: ac.AttemptID})
	checks := 0
	se.BeforeSideEffect = func(context.Context) error {
		checks++
		if checks == 2 {
			require.NoError(t, store.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}))
		}
		return killSwitchCheck(store, ac)
	}
	filler := &stubFiller{fillOK: true, submitReady: true}
	result, err := se.ExecuteStep(context.Background(), makeSnap(textField("f0", "First Name")), singleFieldInterp("f0", ActionFill), filler)
	require.ErrorIs(t, err, errEngineModeReverted)
	assert.Equal(t, StepOutcomeError, result.Outcome)
	assert.Equal(t, 1, filler.fillCalls)
	assert.Zero(t, filler.submitClicks)
}

func TestAdaptiveConfirmation_CancellationAndDelayedEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	assert.False(t, waitAdaptiveConfirmation(ctx, func() bool { calls++; return true }))
	assert.Zero(t, calls)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.True(t, waitAdaptiveConfirmation(ctx, func() bool { calls++; return calls == 2 }))
	assert.Equal(t, 2, calls)
}
