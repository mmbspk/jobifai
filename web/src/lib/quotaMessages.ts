/** User-facing copy when AI credits are exhausted. */
export function quotaBlockMessage(blockCode?: string, plan?: string): string {
  if (blockCode === 'trial_expired') {
    return 'Your trial period has ended. Subscribe or buy credits to keep using AI features.'
  }
  if (blockCode === 'trial_exhausted' || plan === 'trial') {
    return 'Trial credits are used up. Subscribe or buy credits to continue.'
  }
  if (blockCode === 'subscription_expired' || plan === 'expired') {
    return 'Your paid subscription has ended. Choose Starter or Pro to restore monthly credits.'
  }
  if (blockCode === 'past_due') {
    return 'Your last payment did not succeed. Open Manage billing in Stripe to update your payment method.'
  }
  return 'Monthly credits are used up. Buy a top-up or wait for your billing period to renew.'
}
