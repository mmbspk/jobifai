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

// testResourceService constructs a resource.Service with real dependencies.
func testResourceService(t *testing.T) (*resource.Service, *quota.Service, string) {
	t.Helper()
	db := testDB(t)
	quotaSvc, userID := testQuotaService(t, db)
	cfg := config.NewStore(db)
	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	return svc, quotaSvc, userID
}

// TestRecordOnce_HappyPath verifies event + quota burn commit atomically.
func TestRecordOnce_HappyPath(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:v1",
		Quantity:       1,
		Unit:           "render",
		CreditsBurned:  3,
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// Trial bucket should be decremented by 3
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits)
}

// TestRecordOnce_Idempotent verifies the second call with the same key is a no-op.
func TestRecordOnce_Idempotent(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	input := resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:idem",
		Quantity:       1,
		Unit:           "render",
		CreditsBurned:  3,
		Success:        true,
	}
	require.NoError(t, svc.RecordOnce(context.Background(), input))
	require.NoError(t, svc.RecordOnce(context.Background(), input)) // duplicate — must be no-op
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// Only burned once
	assert.Equal(t, int64(500-3), row.TrialRemainingCredits)
}

// TestRecordOnce_FailedOperationZeroCredits verifies failed events store 0 credits.
func TestRecordOnce_FailedOperationZeroCredits(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:fail",
		CreditsBurned:  0,
		ErrorCode:      "render_failed",
		Success:        false,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "failed op must not burn credits")
}

// TestRecordOnce_AdminSkipsBurn verifies admin users are never charged.
func TestRecordOnce_AdminSkipsBurn(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 500, TrialDays: 7,
	}))
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
		CreditsBurned:  100,
		Success:        true,
	}))
	row, err := quotaSvc.RowForUser(adminID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits, "admin must not be charged")
}

// TestBefore_TrialActive verifies Before allows usage when trial has balance.
func TestBefore_TrialActive(t *testing.T) {
	t.Parallel()
	svc, _, userID := testResourceService(t)
	assert.NoError(t, svc.Before(context.Background(), userID, 5))
}

// TestBefore_TrialExhausted verifies Before blocks usage when trial is empty.
func TestBefore_TrialExhausted(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	// Drain all trial credits
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "drain", CreditsBurned: 500, Success: true,
	}))
	row, _ := quotaSvc.RowForUser(userID)
	assert.Equal(t, int64(0), row.TrialRemainingCredits)
	err := svc.Before(context.Background(), userID, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded), "must be quota.ErrExceeded")
}

// TestBefore_ZeroCredits verifies Before always allows 0-credit calls.
func TestBefore_ZeroCredits(t *testing.T) {
	t.Parallel()
	svc, _, userID := testResourceService(t)
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

// TestRecordOnce_ConcurrentSameKey verifies exactly one burn under concurrent retries.
func TestRecordOnce_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	input := resource.RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:concurrent",
		Quantity:       1,
		Unit:           "render",
		CreditsBurned:  5,
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
	assert.Equal(t, int64(500-5), row.TrialRemainingCredits, "concurrent duplicates must burn exactly once")
}

// TestRecordOnce_DifferentKeys verifies distinct keys each burn independently.
func TestRecordOnce_DifferentKeys(t *testing.T) {
	t.Parallel()
	svc, quotaSvc, userID := testResourceService(t)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:k1", CreditsBurned: 3, Success: true,
	}))
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:k2", CreditsBurned: 2, Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500-5), row.TrialRemainingCredits)
}

// TestRecordOnce_TopUpConsumed verifies top-up bucket is used when period is empty.
func TestRecordOnce_TopUpConsumed(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, StarterCreditsMonthly: 10,
	}))
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
	// Drain the period allowance first
	require.NoError(t, quotaSvc.RecordLLMBurn(context.Background(), userID, 10))

	svc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	require.NoError(t, svc.RecordOnce(context.Background(), resource.RecordInput{
		UserID: userID, ResourceType: domain.ResourceTypeDocumentRender,
		IdempotencyKey: "render:topup", CreditsBurned: 5, Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(95), row.TopUpCreditsRemaining, "top-up must fund resource after period exhausted")
}

// TestRecordOnce_EnforcementDisabled verifies disabled enforcement skips burn.
func TestRecordOnce_EnforcementDisabled(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: false, CreditsPerUSD: 1000, TrialCredits: 500, TrialDays: 7,
	}))
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
		IdempotencyKey: "browser:noenforce", CreditsBurned: 999, Success: true,
	}))
	row, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	// No enforcement → no deduction
	assert.Equal(t, int64(500), row.TrialRemainingCredits)
}
