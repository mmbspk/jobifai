package quota

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestRepairReconcileAllowance_RaisesUnderAllocation(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	start := int64(1_700_000_000)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	_, err := sut.db.Exec(`UPDATE user_quota SET period_allowance_micro = 999, period_used_micro = 500, topup_credits_remaining = 200 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	require.NoError(t, sut.RepairReconcileAllowance(uid, domain.QuotaPlanStarter))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, int64(3000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(500), row.PeriodUsedCredits)
	assert.Equal(t, int64(200), row.TopUpCreditsRemaining)
}

func TestRepairReconcileAllowance_PreservesPostDowngradeAllowance(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	start := int64(1_700_000_000)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanPro, PriceID: "price_pro", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 8000,
	}))
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e2", EventCreatedUnix: 101,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		PeriodStartUnix: start, PeriodEndUnix: start + 86400*30, AllowanceCredits: 3000,
	}))
	_, err := sut.db.Exec(`UPDATE user_quota SET period_used_micro = 1200, topup_credits_remaining = 300 WHERE user_id = ?`, uid)
	require.NoError(t, err)

	require.NoError(t, sut.RepairReconcileAllowance(uid, domain.QuotaPlanStarter))
	row, err := sut.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, int64(8000), row.PeriodAllowanceCredits)
	assert.Equal(t, int64(1200), row.PeriodUsedCredits)
	assert.Equal(t, int64(300), row.TopUpCreditsRemaining)
}

func TestRepairReconcileAllowance_IgnoresNonPaidPlans(t *testing.T) {
	t.Parallel()
	sut, _, uid := stripeTestService(t)
	require.NoError(t, sut.InitTrial(context.Background(), uid))
	require.NoError(t, sut.RepairReconcileAllowance(uid, domain.QuotaPlanTrial))
	row, _ := sut.RowForUser(uid)
	assert.Equal(t, int64(500), row.TrialRemainingCredits)
}
