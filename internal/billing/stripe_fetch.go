package billing

import (
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/subscription"
)

// fetchSubscription loads authoritative subscription state from Stripe (overridable in tests).
var fetchSubscription = func(subID string) (stripe.Subscription, error) {
	stripe.Key = StripeSecretKey()
	sub, err := subscription.Get(subID, nil)
	if err != nil {
		return stripe.Subscription{}, err
	}
	return *sub, nil
}

// SetFetchSubscriptionForTest overrides Stripe subscription fetch (tests only).
func SetFetchSubscriptionForTest(fn func(string) (stripe.Subscription, error)) {
	if fn == nil {
		fetchSubscription = func(subID string) (stripe.Subscription, error) {
			stripe.Key = StripeSecretKey()
			sub, err := subscription.Get(subID, nil)
			if err != nil {
				return stripe.Subscription{}, err
			}
			return *sub, nil
		}
		return
	}
	fetchSubscription = fn
}
