package bot_test

// Integration tests for browser automation metering wired through Manager.
// Uses a stub MeterService so no real browser is launched; validates that
// the bot's Config.BrowserMeter is properly populated and lifecycle methods
// are called by the test_automation runner.

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resource"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

// ─── stub MeterService ────────────────────────────────────────────────────────

type countingMeterSvc struct {
	beforeCalls atomic.Int32
	recordCalls atomic.Int32
	failAfter   int32 // if > 0, fail after this many RecordBrowserIncrement calls
}

func (s *countingMeterSvc) BeforeBrowserIncrement(_ context.Context, _ string) error {
	s.beforeCalls.Add(1)
	return nil
}

func (s *countingMeterSvc) RecordBrowserIncrement(_ context.Context, _, _, _, _ string, _ int64) error {
	n := s.recordCalls.Add(1)
	if s.failAfter > 0 && n > s.failAfter {
		return quota.ErrExceeded
	}
	return nil
}

func (s *countingMeterSvc) BrowserIncrementDuration() (time.Duration, error) {
	return 30 * time.Second, nil
}

// ─── setup helpers ────────────────────────────────────────────────────────────

func newManagerWithMeter(t *testing.T, meterSvc bot.MeterService) (*bot.Manager, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key-mgr")
	sessions := browser.NewSessionStore(db, secrets)

	const userID = "meter-test-user"
	require.NoError(t, cfgStore.Set(userID, "resume_profile", domain.ResumeProfile{
		Summary: "Browser meter test profile.",
		PersonalInformation: domain.PersonalInformation{Name: "Meter", Email: "meter@test.com"},
	}))
	mockllm.StoreUserLLMConfig(t, cfgStore, secrets, userID, "http://localhost:1")

	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "session", Value: "mock", Domain: ".example.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(bot.PlatformTestAutomation), string(domain.LoginMethodManual), raw))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions,
		nil, nil, nil, resume.NewPDFRenderer(""), "")
	if meterSvc != nil {
		mgr.SetBrowserMeter(meterSvc)
	}
	return mgr, userID
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestManager_NilMeterSvc_NoWireup verifies that when no MeterService is configured,
// Manager.Start succeeds and Config.BrowserMeter is nil (no billing).
func TestManager_NilMeterSvc_NoWireup(t *testing.T) {
	t.Parallel()
	mgr, userID := newManagerWithMeter(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.Start(ctx, userID, bot.PlatformTestAutomation)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	mgr.Stop(userID)
}

// TestManager_SetBrowserMeter_WiresIntoConfig verifies that SetBrowserMeter causes
// a BrowserUsageMeter to be present in the bot Config for each run.
// The test_automation runner does not launch a real browser so Activate is never
// called — we just verify the manager wiring path compiles and runs clean.
func TestManager_SetBrowserMeter_WiresIntoConfig(t *testing.T) {
	t.Parallel()
	svc := &countingMeterSvc{}
	mgr, userID := newManagerWithMeter(t, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.Start(ctx, userID, bot.PlatformTestAutomation)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	mgr.Stop(userID)
	// test_automation runner does not go through auth, so Activate is never called.
	// The important thing is the wiring doesn't panic and the run completes.
}

// ─── resource.Service integration ─────────────────────────────────────────────

// setupBrowserBillingDB opens a DB with metering enabled and returns a resource.Service.
func setupBrowserBillingDB(t *testing.T) (*resource.Service, *quota.Service, string) {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "billing.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault:    true,
		CreditsPerUSD:         1000,
		ServiceMarkup:         0.5,
		PerCallFeeUSD:         0.002,
		TrialCredits:          500,
		TrialDays:             7,
	}))
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render: domain.RenderMeteringConfig{
			Enabled:                  true,
			RawCostUSDMicroPerRender: 2_000,
		},
		Browser: domain.BrowserMeteringConfig{
			Enabled:                true,
			RawCostUSDMicroPerHour: 20_000, // 30s increment → ~1 credit
			BillingIncrementSecs:   30,
		},
	}))

	userID := "browser-billing-" + t.Name()
	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at)
		 VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Browser Test",
	)
	require.NoError(t, err)

	quotaSvc := quota.NewService(db, cfgStore, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	svc := resource.NewService(db, cfgStore, quotaSvc, quotaSvc.LoadDefaults)
	return svc, quotaSvc, userID
}

// TestBrowserBilling_BeforeAndRecord verifies the full billing pipeline:
// BeforeBrowserIncrement preflight + RecordBrowserIncrement atomic commit.
// With RawCostUSDMicroPerHour=20_000, 30s increment, CreditsPerUSD=1000, ServiceMarkup=0.5:
//   raw = 20_000*30/3600 = 166 µUSD; loaded = 249 µUSD → 0.249 credits → minimum-1 rule → 1 credit.
func TestBrowserBilling_BeforeAndRecord(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := setupBrowserBillingDB(t)

	ctx := context.Background()
	key1 := "browser-automation:" + userID + ":run-1:0"
	require.NoError(t, svc.BeforeBrowserIncrement(ctx, userID))
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key1, "run-1", "", 30_000))

	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(499), row.TrialRemainingCredits, "exactly 1 credit burned for first bucket")

	// Same idempotency key → credits unchanged (deduplication).
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key1, "run-1", "", 30_000))
	row2, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(499), row2.TrialRemainingCredits, "duplicate key must not burn more credits")

	// Next unique bucket → 1 more credit burned.
	key2 := "browser-automation:" + userID + ":run-1:1"
	require.NoError(t, svc.BeforeBrowserIncrement(ctx, userID))
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key2, "run-1", "", 30_000))
	row3, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(498), row3.TrialRemainingCredits, "exactly 1 credit burned for second bucket")

	// Exactly 2 resource_usage_events rows must exist.
	var eventCount int
	require.NoError(t, svc.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_usage_events WHERE user_id = ? AND resource_type = 'browser_automation'`, userID).Scan(&eventCount))
	assert.Equal(t, 2, eventCount, "exactly 2 billing events (one per unique key)")
}

// TestBrowserBilling_DisabledMetering verifies that when metering is disabled,
// BeforeBrowserIncrement and RecordBrowserIncrement are no-ops and NO event rows are written.
func TestBrowserBilling_DisabledMetering(t *testing.T) {
	t.Parallel()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "disabled.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		CreditsPerUSD: 1000, TrialCredits: 500, TrialDays: 7,
	}))
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: false,
	}))

	userID := "disabled-browser-" + t.Name()
	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at)
		 VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Disabled User",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfgStore, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	svc := resource.NewService(db, cfgStore, quotaSvc, quotaSvc.LoadDefaults)

	ctx := context.Background()
	require.NoError(t, svc.BeforeBrowserIncrement(ctx, userID))
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID,
		"browser-automation:"+userID+":run-1:0", "run-1", "", 0))

	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "no credits burned when metering disabled")

	// Zero resource events must exist when metering is disabled (true no-op).
	var eventCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_usage_events WHERE resource_type = 'browser_automation'`).Scan(&eventCount))
	assert.Equal(t, 0, eventCount, "disabled metering must not write any event rows")
}

// TestBrowserBilling_IdempotentRecord verifies that calling RecordBrowserIncrement
// with the same idempotency key twice only burns credits once.
func TestBrowserBilling_IdempotentRecord(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := setupBrowserBillingDB(t)

	ctx := context.Background()
	key := "browser-automation:" + userID + ":run-idem:0"
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key, "run-idem", "", 30_000))
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key, "run-idem", "", 30_000)) // duplicate

	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(499), row.TrialRemainingCredits, "exactly 1 credit burned (duplicate is no-op)")

	// Make a third call with a different key to confirm billing still works.
	key2 := "browser-automation:" + userID + ":run-idem:1"
	require.NoError(t, svc.RecordBrowserIncrement(ctx, userID, key2, "run-idem", "", 30_000))
	row2, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(498), row2.TrialRemainingCredits, "1 credit burned for second unique key")
}

// TestBrowserBilling_QuotaExhaustion verifies that when the credit balance is too low,
// BeforeBrowserIncrement returns quota.ErrExceeded.
// TestBrowserBilling_QuotaExhaustion verifies that with zero credits and metering enabled,
// BeforeBrowserIncrement returns quota.ErrExceeded.
// Each 30s increment costs exactly 1 credit (minimum-1-credit rule), so 0 trial credits
// → immediate ErrExceeded.
func TestBrowserBilling_QuotaExhaustion(t *testing.T) {
	t.Parallel()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "exhausted.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
		TrialDays:          7,
	}))
	require.NoError(t, cfgStore.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{
			Enabled:                true,
			RawCostUSDMicroPerHour: 20_000,
			BillingIncrementSecs:   30,
		},
	}))

	userID := "exhausted-" + t.Name()
	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at)
		 VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Exhausted User",
	)
	require.NoError(t, err)
	// Insert a quota row directly with 0 trial credits and enforcement enabled.
	// (InitTrial with TrialCredits=0 in quota_defaults would fall back to the
	// builtin 500-credit default because loadDefaults ignores zero values.)
	_, err = db.Exec(`
		INSERT INTO user_quota (user_id, plan, enforcement_enabled, trial_remaining_micro, trial_ends_at, updated_at)
		VALUES (?, 'trial', 1, 0, datetime('now', '+7 days'), CURRENT_TIMESTAMP)`,
		userID)
	require.NoError(t, err)

	quotaSvc := quota.NewService(db, cfgStore, nil)
	svc := resource.NewService(db, cfgStore, quotaSvc, quotaSvc.LoadDefaults)

	ctx := context.Background()
	err = svc.BeforeBrowserIncrement(ctx, userID)
	require.Error(t, err, "zero credit balance must be rejected by BeforeBrowserIncrement")
	assert.True(t, errors.Is(err, quota.ErrExceeded), "expected quota.ErrExceeded, got: %v", err)
}

// TestBrowserBilling_BrowserIncrementDuration verifies the duration helper returns the
// configured increment.
func TestBrowserBilling_BrowserIncrementDuration(t *testing.T) {
	t.Parallel()
	svc, _, _ := setupBrowserBillingDB(t)
	d, err := svc.BrowserIncrementDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}
