package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v86"
)

// Documents the Stripe API version baked into the installed stripe-go SDK (webhook ConstructEvent expects this release train).
func TestStripeSDK_APIVersion(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, stripe.APIVersion)
	t.Logf("stripe-go ClientVersion=%s APIVersion=%s", stripe.ClientVersion, stripe.APIVersion)
}
