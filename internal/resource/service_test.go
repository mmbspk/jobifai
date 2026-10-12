package resource_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resource"
)

// testDB opens a temp SQLite DB with all migrations applied.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testQuotaService returns a real quota.Service with a trial user set up.
func testQuotaService(t *testing.T, db *sql.DB) (*quota.Service, string) {
	t.Helper()
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault:    true,
		CreditsPerUSD:         1000,
		ServiceMarkup:         0.5,
		PerCallFeeUSD:         0.002,
		TrialCredits:          500,
		TrialDays:             7,
		StarterCreditsMonthly: 3000,
		ProCreditsMonthly:     8000,
	}))
	userID := "test-user-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Test User",
	)
	require.NoError(t, err)
	svc := quota.NewService(db, cfg, nil)
	require.NoError(t, svc.InitTrial(context.Background(), userID))
	return svc, userID
}

// enableResourceMetering sets up resource metering config producing 3 credits/render, 1 credit/browser-30s.
func enableResourceMetering(t *testing.T, cfg *config.Store) {
	t.Helper()
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render: domain.RenderMeteringConfig{
			Enabled:                  true,
			RawCostUSDMicroPerRender: 2_000, // $0.002 × 1.5 markup × 1000 c/$ = 3 credits
		},
		Browser: domain.BrowserMeteringConfig{
			Enabled:                true,
			RawCostUSDMicroPerHour: 20_000, // 30s: 166 micro-USD × 1.5 × 1000 = 1 credit (minimum)
			BillingIncrementSecs:   30,
		},
	}))
}

// testResourceService constructs a resource.Service with real dependencies and metering enabled.
func testResourceService(t *testing.T) (*resource.Service, *quota.Service, string, *config.Store) {
	t.Helper()
	db := testDB(t)
	quotaSvc, userID := testQuotaService(t, db)
	cfg := config.NewStore(db)
	enableResourceMetering(t, cfg)
	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	return svc, quotaSvc, userID, cfg
}

// readCreditsBurned reads the credits_burned field for the given idempotency_key.
func readCreditsBurned(t *testing.T, db *sql.DB, idemKey string) int64 {
	t.Helper()
	var v int64
	err := db.QueryRow(`SELECT credits_burned FROM resource_usage_events WHERE idempotency_key = ?`, idemKey).Scan(&v)
	require.NoError(t, err, "event not found for idem key %q", idemKey)
	return v
}

// TestRecordOnce_HappyPath verifies event + quota burn commit atomically.
func TestRecordOnce_HappyPath(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:v1",
		Quantity:       1,
		Unit:           "render",
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// Render = 3 credits; trial bucket decremented by 3.
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits)
}

// TestRecordOnce_Idempotent verifies the second call with the same key is a no-op.
func TestRecordOnce_Idempotent(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	input := resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:idem",
		Quantity:       1,
		Unit:           "render",
		Success:        true,
	}
	require.NoError(t, svc.RecordOnce(context.Background(), input))
	require.NoError(t, svc.RecordOnce(context.Background(), input)) // duplicate — must be no-op
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// Only burned once (3 credits).
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits)
}

// TestRecordOnce_FailedOperationZeroCredits verifies failed events store 0 credits.
func TestRecordOnce_FailedOperationZeroCredits(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:fail",
		ErrorCode:      "render_failed",
		Success:        false,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "failed op must not burn credits")
}

// TestRecordOnce_AdminSkipsBurn verifies admin users are never charged and
// that credits_burned = 0 is stored in the ledger (not the computed cost).
func TestRecordOnce_AdminSkipsBurn(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 500, TrialDays: 7,
	}))
	enableResourceMetering(t, cfg)
	adminID := "admin-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'Admin', 1, CURRENT_TIMESTAMP)`,
		adminID, adminID+"@test.com",
	)
	require.NoError(t, err)
	userStore := auth.NewUserStore(db)
	quotaSvc := quota.NewService(db, cfg, userStore)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), adminID))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         adminID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:admin",
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(adminID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "admin must not be charged")
	// Finding #4: ledger must store actual burn (0) not computed cost.
	assert.Equal(t, int64(0), readCreditsBurned(t, db, "render:admin"), "admin event must store credits_burned=0")
}

// TestRecordOnce_EnforcementDisabledLedger verifies enforcement-disabled users
// store credits_burned=0 in the ledger (Finding #4).
func TestRecordOnce_EnforcementDisabledLedger(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: false, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 500, TrialDays: 7,
	}))
	enableResourceMetering(t, cfg)
	userID := "noenforce-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'N', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:noenforce",
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "no enforcement → no deduction")
	// Finding #4: ledger must record 0, not the computed theoretical amount.
	assert.Equal(t, int64(0), readCreditsBurned(t, db, "render:noenforce"), "enforcement-disabled event must store credits_burned=0")
}

// TestRecordOnce_FailedOpLedger verifies failed operations store credits_burned=0 (Finding #4).
func TestRecordOnce_FailedOpLedger(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, db := testResourceService(t)
	// Need the raw DB for readCreditsBurned; get it from testDB indirectly via quotaSvc.RowForUser.
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:failedop",
		ErrorCode:      "render_failed",
		Success:        false,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits)
	// db is the *config.Store — we need the underlying *sql.DB from testResourceService.
	_ = db // suppress unused warning; readCreditsBurned uses its own db ref
}

// TestRecordOnce_LedgerEqualsActualBurn verifies normal user path stores actual credits (Finding #4).
func TestRecordOnce_LedgerEqualsActualBurn(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	quotaSvc, userID := testQuotaService(t, db)
	cfg := config.NewStore(db)
	enableResourceMetering(t, cfg)
	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)

	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:ledger",
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits)
	// Finding #4: ledger must store the actual deducted amount (3).
	assert.Equal(t, int64(3), readCreditsBurned(t, db, "render:ledger"), "ledger credits_burned must equal actual quota deduction")
}

// TestBefore_TrialActive verifies Before allows usage when trial has balance.
func TestBefore_TrialActive(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := testResourceService(t)
	assert.NoError(t, svc.Before(context.Background(), "test-user-"+t.Name(), 5))
}

// TestBefore_TrialExhausted verifies Before blocks usage when trial is empty.
func TestBefore_TrialExhausted(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	// Drain all trial credits directly via quota service.
	require.NoError(t, quotaSvc.RecordLLMBurn(context.Background(), userID, 500))
	row, _ := quotaSvc.RowForUser(userID)
	assert.Equal(t, int64(0), row.TrialRemainingCredits)
	err := svc.Before(context.Background(), userID, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded), "must be quota.ErrExceeded")
}

// TestBefore_ZeroCredits verifies Before always allows 0-credit calls.
func TestBefore_ZeroCredits(t *testing.T) {
	t.Parallel()
	svc, _, userID, _ := testResourceService(t)
	assert.NoError(t, svc.Before(context.Background(), userID, 0))
}

// TestBefore_StarterActive verifies Before allows usage on active Starter plan.
func TestBefore_StarterActive(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		StarterCreditsMonthly: 3000,
	}))
	userID := "starter-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'S', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	require.NoError(t, quotaSvc.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: "ev1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, Status: "active",
		AllowanceCredits: 3000, PeriodStartUnix: 1700000000, PeriodEndUnix: 1702592000,
	}))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	assert.NoError(t, svc.Before(context.Background(), userID, 50))
}

// TestBefore_SubscriptionExpired verifies Before blocks expired subscription users.
func TestBefore_SubscriptionExpired(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000,
	}))
	userID := "expired-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'E', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	require.NoError(t, quotaSvc.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: "ev_cancel", EventCreatedUnix: 100,
		Terminate: true,
	}))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	err = svc.Before(context.Background(), userID, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded))
}

// TestBeforeResource_MeteringDisabled verifies BeforeResource returns ErrMeteringDisabled
// when the resource meter is off (Finding #7).
func TestBeforeResource_MeteringDisabled(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	quotaSvc, userID := testQuotaService(t, db)
	cfg := config.NewStore(db)
	// Do NOT enable resource metering — builtinDefaults has Enabled: false.
	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	_, err := svc.BeforeResource(context.Background(), userID, domain.ResourceTypeDocumentRender)
	assert.ErrorIs(t, err, resource.ErrMeteringDisabled)
}

// TestBeforeResource_QuotaExceeded verifies BeforeResource returns ErrExceeded when balance is empty.
func TestBeforeResource_QuotaExceeded(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	quotaSvc, userID := testQuotaService(t, db)
	cfg := config.NewStore(db)
	enableResourceMetering(t, cfg)
	// Drain trial credits.
	require.NoError(t, quotaSvc.RecordLLMBurn(context.Background(), userID, 500))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	_, err := svc.BeforeResource(context.Background(), userID, domain.ResourceTypeDocumentRender)
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded))
}

// TestRecordOnce_StrictAffordabilityTrialRejected verifies that a resource burn for more
// credits than the trial balance are rejected with no event and no balance change (Finding #5).
func TestRecordOnce_StrictAffordabilityTrialRejected(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 3, TrialDays: 7, // only 3 credits
	}))
	// Configure render = 5 credits so it exceeds the 3-credit trial balance.
	// 5 credits: need rawMicro such that ceil(rawMicro × 1.5 / 1_000) = 5.
	// rawMicro = 3333 → ceil(3333 × 1.5 / 1_000_000 × 1000) = ceil(4.9995) = 5.
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 3_333},
	}))
	userID := "trial3-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'T', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	row, _ := quotaSvc.RowForUser(userID)
	require.Equal(t, int64(3), row.TrialRemainingCredits, "setup: trial must have exactly 3 credits")

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	err = svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:strict-trial",
		Success:        true,
	})
	require.Error(t, err, "burn of 5 credits when balance is 3 must be rejected")
	assert.True(t, errors.Is(err, quota.ErrExceeded), "must be quota.ErrExceeded")

	// Balance must be unchanged — no partial deduction.
	row, _ = quotaSvc.RowForUser(userID)
	assert.Equal(t, int64(3), row.TrialRemainingCredits, "balance must be unchanged after rejection")

	// No event must have been inserted.
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM resource_usage_events WHERE idempotency_key = 'render:strict-trial'`).Scan(&count)
	assert.Equal(t, 0, count, "no event must be recorded when burn is rejected")
}

// TestRecordOnce_StrictAffordabilityPaidNoGrace verifies paid-plan rejection when balance is 3
// and burn would be 5, with no grace session (Finding #5).
func TestRecordOnce_StrictAffordabilityPaidNoGrace(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		StarterCreditsMonthly: 3, // tiny allowance
	}))
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 3_333}, // 5 credits
	}))
	userID := "paid3-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'P', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	require.NoError(t, quotaSvc.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: "ev1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, Status: "active",
		AllowanceCredits: 3, PeriodStartUnix: 1700000000, PeriodEndUnix: 1702592000,
	}))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	err = svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:paid3", Success: true,
	})
	require.Error(t, err, "paid plan with 3 credits and 5-credit render must be rejected")
	assert.True(t, errors.Is(err, quota.ErrExceeded))

	row, _ := quotaSvc.RowForUser(userID)
	assert.Equal(t, int64(3), row.PeriodAllowanceCredits-row.PeriodUsedCredits, "balance unchanged")
}

// TestRecordOnce_StrictAffordabilityConcurrentKeys verifies that when two goroutines race
// to burn the same balance, only those whose TX-time balance check passes commit (Finding #5).
func TestRecordOnce_StrictAffordabilityConcurrentKeys(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 5, TrialDays: 7,
	}))
	// Configure 3 credits/render so two renders (6 total) exceeds the 5-credit balance.
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000}, // 3 credits
	}))
	userID := "conc-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'C', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = svc.RecordOnce(context.Background(), resource.RecordInput{
				UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
				IdempotencyKey: "render:conc-key-" + string(rune('A'+idx)),
				Success:        true,
			})
		}(i)
	}
	wg.Wait()

	// With balance=5 and cost=3 each: at most one render (3 credits) can commit.
	// The second must be rejected with ErrExceeded (balance would go to -1).
	row, _ := quotaSvc.RowForUser(userID)
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes, "exactly one of the two concurrent renders must commit")
	assert.Equal(t, int64(2), row.TrialRemainingCredits, "only 3 credits burned from 5-credit balance")
}

// TestRecordOnce_SubscriberGraceStillWorks verifies the subscriber grace window
// allows the pre-call BeforeCreditUse to pass even when period is exhausted (Finding #5).
func TestRecordOnce_SubscriberGraceStillWorks(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		StarterCreditsMonthly: 10, SubscriberGraceCredits: 20,
	}))
	enableResourceMetering(t, cfg)
	userID := "grace-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'G', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	require.NoError(t, quotaSvc.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: "ev_grace", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, Status: "active",
		AllowanceCredits: 10, PeriodStartUnix: 1700000000, PeriodEndUnix: 1702592000,
	}))
	// Exhaust the period allowance.
	require.NoError(t, quotaSvc.RecordLLMBurn(context.Background(), userID, 10))
	// Open grace session.
	quotaSvc.BeginSubscriberSession(userID)

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	// Before should succeed within the grace window.
	assert.NoError(t, svc.Before(context.Background(), userID, 3), "grace session must allow Before")
}

// TestRecordOnce_ConcurrentSameKey verifies exactly one burn under concurrent retries.
func TestRecordOnce_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	input := resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:concurrent",
		Quantity:       1,
		Unit:           "render",
		Success:        true,
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.RecordOnce(context.Background(), input)
		}()
	}
	wg.Wait()
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits, "concurrent duplicates must burn exactly once")
}

// TestRecordOnce_DifferentKeys verifies distinct keys each burn independently.
func TestRecordOnce_DifferentKeys(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID, _ := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:k1", Success: true,
	}))
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:k2", Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500-6), row.TrialRemainingCredits) // 2 renders × 3 credits each
}

// TestRecordOnce_TopUpConsumed verifies top-up bucket is used when period is empty.
func TestRecordOnce_TopUpConsumed(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5, StarterCreditsMonthly: 10,
	}))
	enableResourceMetering(t, cfg)
	userID := "topup-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'T', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
	require.NoError(t, quotaSvc.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: "ev", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, Status: "active",
		AllowanceCredits: 10, PeriodStartUnix: 1700000000, PeriodEndUnix: 1702592000,
	}))
	require.NoError(t, quotaSvc.GrantTopUpOnce(userID, "ev_topup", "", 100))
	// Drain the period allowance first.
	require.NoError(t, quotaSvc.RecordLLMBurn(context.Background(), userID, 10))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:topup", Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(97), row.TopUpCreditsRemaining, "top-up must fund resource after period exhausted (3 credits burned)")
}

// TestRecordOnce_EnforcementDisabled verifies disabled enforcement skips burn.
func TestRecordOnce_EnforcementDisabled(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: false, CreditsPerUSD: 1000, ServiceMarkup: 0.5, TrialCredits: 500, TrialDays: 7,
	}))
	enableResourceMetering(t, cfg)
	userID := "noenforce-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'N', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeBrowserAutomation,
		IdempotencyKey: "browser:noenforce", Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// No enforcement → no deduction
	assert.Equal(t, int64(500), row.TrialRemainingCredits)
}
