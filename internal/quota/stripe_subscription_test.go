package quota

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func stripeTestService(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	sqldb, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, keyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault:    true,
		TrialCredits:          500,
		TrialDays:             7,
		StarterCreditsMonthly: 3000,
		ProCreditsMonthly:     8000,
		StripePriceStarter:    "price_starter",
		StripePricePro:        "price_pro",
	}))
	_, err = sqldb.Exec(`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES ('u1','a@t.com','x','A',0,CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	sut := NewService(sqldb, cfg, nil)
	require.NoError(t, sut.InitTrial(context.Background(), "u1"))
	return sut, sqldb, "u1"
}

func TestApplyStripeSubscription_NewPeriodResetsUsageOnce(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	start := time.Now().Unix()
	end := start + 86400 * 30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100, Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: end, AllowanceCredits: 3000,
	}))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, int64(3000), row.PeriodAllowanceCredits)

	_, err = sqldb.Exec(`UPDATE user_quota SET period_used_micro = 2500, topup_credits_remaining = 100 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 101, Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: end, AllowanceCredits: 3000,
	}))
	row2, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(2500), row2.PeriodUsedCredits)
	assert.Equal(t, int64(100), row2.TopUpCreditsRemaining)

	newStart := start + 86400 * 31
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e3", EventCreatedUnix: 300, Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
		Status: "active", PeriodStartUnix: newStart, PeriodEndUnix: end + 86400*31, AllowanceCredits: 3000,
	}))
	row3, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(0), row3.PeriodUsedCredits)
	assert.Equal(t, int64(0), row3.TopUpCreditsRemaining)
}

func TestApplyStripeSubscription_UnknownPriceBlockedAtProcessor(t *testing.T) {
	t.Parallel()
	// Service accepts empty plan skip; processor maps unknown price — covered in billing tests.
	sut, _, uid := stripeTestService(t)
	err := sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", Plan: "", PriceID: "price_unknown", Status: "active",
	})
	require.NoError(t, err)
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanTrial, row.Plan)
}

func TestApplyStripeSubscription_TerminateSetsExpired(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100, Plan: domain.QuotaPlanPro, PriceID: "price_pro",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 8000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET topup_credits_remaining = 250 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 200, Terminate: true, Status: "canceled",
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanExpired, row.Plan)
	assert.Equal(t, int64(0), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(0), row.TopUpCreditsRemaining)
}

func TestApplyStripeSubscription_EqualTimestampDoesNotReviveExpired(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	ts := int64(5000)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "del", EventCreatedUnix: ts, Terminate: true, Status: "canceled",
	}))
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "upd", EventCreatedUnix: ts,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanExpired, row.Plan)
}

func TestApplyStripeSubscription_StaleEventDoesNotRevive(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "del", EventCreatedUnix: 2000, Terminate: true, Status: "canceled",
	}))
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "old_upd", EventCreatedUnix: 1000,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanExpired, row.Plan)
}

func TestApplyStripeSubscription_CancelAtPeriodEndKeepsTopUp(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET topup_credits_remaining = 400 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		CancelAtPeriodEnd: true, PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(400), row.TopUpCreditsRemaining)
	assert.True(t, row.CancelAtPeriodEnd)
}

func TestApplyStripeSubscription_UpgradeSamePeriod(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET period_used_micro = 1200 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanPro, PriceID: "price_pro", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 8000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanPro, row.Plan)
	assert.Equal(t, int64(8000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(1200), row.PeriodUsedCredits)
}

func TestApplyStripeSubscription_DowngradeSamePeriod(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanPro, PriceID: "price_pro", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 8000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET period_used_micro = 5000 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanStarter, row.Plan)
	assert.Equal(t, int64(8000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(5000), row.PeriodUsedCredits)
	assert.Equal(t, int64(0), row.OverageDebtCredits)

	newStart := start + 86400*31
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e3", EventCreatedUnix: 300,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: newStart, PeriodEndUnix: newStart + 86400*30, AllowanceCredits: 3000,
	}))
	row3, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(3000), row3.PeriodAllowanceCredits)
	assert.Equal(t, int64(0), row3.PeriodUsedCredits)
	assert.Equal(t, int64(0), row3.OverageDebtCredits)
}

func TestApplyStripeSubscription_FailedRenewalPastDuePreservesEntitlement(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	oldStart := int64(1_700_000_000)
	oldEnd := oldStart + 86400*30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: oldStart, PeriodEndUnix: oldEnd, AllowanceCredits: 3000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET period_used_micro = 777, topup_credits_remaining = 333 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	newStart := oldStart + 86400*31
	newEnd := newStart + 86400*30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_fail", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "past_due",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, "past_due", row.StripeSubscriptionStatus)
	assert.Equal(t, int64(3000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(777), row.PeriodUsedCredits)
	assert.Equal(t, int64(333), row.TopUpCreditsRemaining)
	require.NotNil(t, row.PeriodStart)
	assert.Equal(t, oldStart, row.PeriodStart.Unix())
	require.NotNil(t, row.PeriodEnd)
	assert.Equal(t, oldEnd, row.PeriodEnd.Unix())

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_fail2", EventCreatedUnix: 201,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "past_due",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	row2, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(777), row2.PeriodUsedCredits)
	assert.Equal(t, int64(333), row2.TopUpCreditsRemaining)
}

func TestApplyStripeSubscription_PastDueRecoveryActiveAdvancesPeriod(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	oldStart := int64(1_700_000_000)
	oldEnd := oldStart + 86400*30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: oldStart, PeriodEndUnix: oldEnd, AllowanceCredits: 3000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET period_used_micro = 777, topup_credits_remaining = 333 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	newStart := oldStart + 86400*31
	newEnd := newStart + 86400*30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_fail", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "past_due",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_paid", EventCreatedUnix: 300,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, "active", row.StripeSubscriptionStatus)
	assert.Equal(t, int64(3000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(0), row.PeriodUsedCredits)
	assert.Equal(t, int64(0), row.TopUpCreditsRemaining)
	require.NotNil(t, row.PeriodStart)
	assert.Equal(t, newStart, row.PeriodStart.Unix())
	require.NotNil(t, row.PeriodEnd)
	assert.Equal(t, newEnd, row.PeriodEnd.Unix())

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_paid_dup", EventCreatedUnix: 301,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	row2, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(0), row2.PeriodUsedCredits)
	assert.Equal(t, int64(0), row2.TopUpCreditsRemaining)
}

func TestApplyStripeSubscription_StalePastDueAfterRecoveryIgnored(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	oldStart := int64(1_700_000_000)
	oldEnd := oldStart + 86400*30
	newStart := oldStart + 86400*31
	newEnd := newStart + 86400*30
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: oldStart, PeriodEndUnix: oldEnd, AllowanceCredits: 3000,
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET period_used_micro = 777, topup_credits_remaining = 333 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_fail", EventCreatedUnix: 200,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "past_due",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_paid", EventCreatedUnix: 300,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e_stale_past_due", EventCreatedUnix: 150,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "past_due",
		PeriodStartUnix: newStart, PeriodEndUnix: newEnd, AllowanceCredits: 3000,
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, "active", row.StripeSubscriptionStatus)
	assert.Equal(t, int64(0), row.PeriodUsedCredits)
	assert.Equal(t, newStart, row.PeriodStart.Unix())
}

func TestGrantTopUpOnce_Idempotent(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	require.NoError(t, sut.GrantTopUpOnce(uid, "evt_1", "cs_1", 500))
	require.NoError(t, sut.GrantTopUpOnce(uid, "evt_1", "cs_1", 500))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(500), row.TopUpCreditsRemaining)
}
