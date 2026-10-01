package quota

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestBeforeLLM_SubscriptionStatusGating(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		plan   string
		status string
		allow  bool
	}{
		{"active_starter", domain.QuotaPlanStarter, "active", true},
		{"trialing_pro", domain.QuotaPlanPro, "trialing", true},
		{"past_due_starter", domain.QuotaPlanStarter, "past_due", true},
		{"canceled_starter", domain.QuotaPlanStarter, "canceled", false},
		{"unpaid_pro", domain.QuotaPlanPro, "unpaid", false},
		{"incomplete", domain.QuotaPlanStarter, "incomplete", false},
		{"incomplete_expired", domain.QuotaPlanStarter, "incomplete_expired", false},
		{"paused", domain.QuotaPlanPro, "paused", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sut, sqldb, uid := stripeTestService(t)
			if tc.allow {
				price := "price_starter"
				if tc.plan == domain.QuotaPlanPro {
					price = "price_pro"
				}
				require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
					UserID: uid, EventID: "e_" + tc.name, EventCreatedUnix: 100,
					Plan: tc.plan, PriceID: price, Status: tc.status,
					PeriodStartUnix: 100, PeriodEndUnix: 999999,
					AllowanceCredits: 3000,
				}))
			} else {
				_, err := sqldb.Exec(`
					UPDATE user_quota SET plan = ?, stripe_subscription_status = ?,
						period_allowance_micro = 3000, period_used_micro = 0
					WHERE user_id = ?`, tc.plan, tc.status, uid)
				require.NoError(t, err)
			}
			err := sut.BeforeLLM(context.Background(), uid, "claude-sonnet-4-6", 100, 100)
			if tc.allow {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBeforeLLM_ExpiredIgnoresTopUpBalance(t *testing.T) {
	t.Parallel()
	sut, sqldb, uid := stripeTestService(t)
	require.NoError(t, sut.ApplyStripeSubscription(StripeSubscriptionUpdate{
		UserID: uid, EventID: "e1", EventCreatedUnix: 2000, Terminate: true, Status: "canceled",
	}))
	_, err := sqldb.Exec(`UPDATE user_quota SET topup_credits_remaining = 5000 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	err = sut.BeforeLLM(context.Background(), uid, "claude-sonnet-4-6", 10, 10)
	require.Error(t, err)
}
