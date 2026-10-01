package quota

import "github.com/user/jobifai/internal/domain"

// TopUpEligible reports whether the user may purchase a top-up pack (server-side gate).
// Requires Starter/Pro with Stripe status active or trialing (not past_due).
func TopUpEligible(row domain.UserQuotaRow) bool {
	if row.Plan != domain.QuotaPlanStarter && row.Plan != domain.QuotaPlanPro {
		return false
	}
	switch row.StripeSubscriptionStatus {
	case "active", "trialing":
		return true
	default:
		return false
	}
}

// BlocksNewSubscriptionCheckout is true when creating a new subscription Checkout would duplicate entitlement.
func BlocksNewSubscriptionCheckout(row domain.UserQuotaRow) bool {
	if row.StripeSubscriptionID == "" {
		return false
	}
	switch row.StripeSubscriptionStatus {
	case "canceled", "incomplete_expired":
		return false
	default:
		// Empty/unknown status with a subscription ID: block until reconcile (legacy rows).
		return true
	}
}
