package quota

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

func TestTopUpEligible(t *testing.T) {
	t.Parallel()
	assert.True(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanStarter, StripeSubscriptionStatus: "active"}))
	assert.True(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanPro, StripeSubscriptionStatus: "trialing"}))
	assert.False(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanTrial}))
	assert.False(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanExpired}))
	assert.False(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanStarter, StripeSubscriptionStatus: "past_due"}))
	assert.False(t, TopUpEligible(domain.UserQuotaRow{Plan: domain.QuotaPlanPro, StripeSubscriptionStatus: "canceled"}))
}

func TestBlocksNewSubscriptionCheckout(t *testing.T) {
	t.Parallel()
	assert.False(t, BlocksNewSubscriptionCheckout(domain.UserQuotaRow{}))
	assert.True(t, BlocksNewSubscriptionCheckout(domain.UserQuotaRow{
		StripeSubscriptionID: "sub_1", StripeSubscriptionStatus: "active",
	}))
	assert.True(t, BlocksNewSubscriptionCheckout(domain.UserQuotaRow{
		StripeSubscriptionID: "sub_legacy", StripeSubscriptionStatus: "",
	}))
	assert.False(t, BlocksNewSubscriptionCheckout(domain.UserQuotaRow{
		StripeSubscriptionID: "sub_1", StripeSubscriptionStatus: "canceled",
	}))
	assert.False(t, BlocksNewSubscriptionCheckout(domain.UserQuotaRow{
		StripeSubscriptionID: "sub_1", StripeSubscriptionStatus: "incomplete_expired",
	}))
}
