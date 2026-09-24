import { apiPost } from './client'

export const billingApi = {
  checkout: (plan: 'starter' | 'pro') => apiPost<{ url: string }>('/billing/checkout', { plan }),
  topUp: (credits: number) => apiPost<{ url: string }>('/billing/topup', { credits }),
  portal: () => apiPost<{ url: string }>('/billing/portal', {}),
}
