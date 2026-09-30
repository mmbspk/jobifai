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
		UserID: uid, EventID: "e1", Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: end, AllowanceCredits: 3000,
	}))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, int64(3000), row.PeriodAllowanceCredits)

	_, err = sqldb.Exec(`UPDATE user_quota SET period_used_micro = 2500, topup_credits_remaining = 100 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: end, AllowanceCredits: 3000,
	}))
	row2, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(2500), row2.PeriodUsedCredits)
	assert.Equal(t, int64(100), row2.TopUpCreditsRemaining)

	newStart := start + 86400 * 31
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e3", Plan: domain.QuotaPlanStarter, PriceID: "price_starter",
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
	sut, _, uid := stripeTestService(t)
	start := time.Now().Unix()
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", Plan: domain.QuotaPlanPro, PriceID: "price_pro",
		Status: "active", PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 8000,
	}))
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", Terminate: true, Status: "canceled",
	}))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanExpired, row.Plan)
	assert.Equal(t, int64(0), row.PeriodAllowanceCredits)
}

func TestGrantTopUpOnce_Idempotent(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	require.NoError(t, sut.GrantTopUpOnce(uid, "evt_1", "cs_1", 500))
	require.NoError(t, sut.GrantTopUpOnce(uid, "evt_1", "cs_1", 500))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(500), row.TopUpCreditsRemaining)
}
