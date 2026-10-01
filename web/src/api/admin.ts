import { apiDelete, apiGet, apiPost, apiPut } from './client'
import type { AdminUserDetail, AdminUserRow, GeneralSettings, LLMOverrides, QuotaUserOverrides } from '../types'

export interface SystemSecretsStatus {
  has_default_api_key: boolean
  has_proxy_key?: boolean
  llm_api_key?: string
}

export const adminApi = {
  system: {
    get: () => apiGet<GeneralSettings>('/admin/system'),
    set: (body: GeneralSettings) => apiPut<void>('/admin/system', body),
  },
  systemSecrets: {
    get: () => apiGet<SystemSecretsStatus>('/admin/system/secrets'),
    setApiKey: (value: string, keyType = 'llm_api_key') =>
      apiPost<void>('/admin/system/secrets/api-key', { key_type: keyType, value }),
    deleteApiKey: (keyType = 'llm_api_key') =>
      apiDelete(`/admin/system/secrets/api-key?key_type=${encodeURIComponent(keyType)}`),
  },
  users: {
    list: () => apiGet<AdminUserRow[]>('/admin/users'),
    get: async (userId: string) => {
      const res = await apiGet<{
        user: AdminUserDetail
        usage_summary?: {
          llm_calls: number
          credits_burned: number
          raw_cost_usd_micro: number
          loaded_cost_usd_micro: number
        }
      }>(`/admin/users/${userId}`)
      return { ...res.user, usage_summary: res.usage_summary }
    },
    update: (
      userId: string,
      body: { is_admin?: boolean; verbose_logs?: boolean; llm_overrides?: LLMOverrides; quota_overrides?: QuotaUserOverrides },
    ) => apiPut<void>(`/admin/users/${userId}`, body),
    setApiKey: (userId: string, value: string) =>
      apiPost<void>(`/admin/users/${userId}/secrets/api-key`, { value }),
    deleteApiKey: (userId: string) =>
      apiDelete(`/admin/users/${userId}/secrets/api-key`),
    delete: (userId: string) => apiDelete(`/admin/users/${userId}`),
    pruneE2E: () => apiPost<{ deleted: number }>('/admin/users/prune-e2e', {}),
  },
  billing: {
    summary: () => apiGet<AdminBillingSummary>('/admin/billing/summary'),
    users: () => apiGet<AdminUserBillingRow[]>('/admin/billing/users'),
    webhookEvents: (params?: { status?: string; event_type?: string; user_id?: string; limit?: number }) => {
      const q = new URLSearchParams()
      if (params?.status) q.set('status', params.status)
      if (params?.event_type) q.set('event_type', params.event_type)
      if (params?.user_id) q.set('user_id', params.user_id)
      if (params?.limit) q.set('limit', String(params.limit))
      const qs = q.toString()
      return apiGet<AdminWebhookEventRow[]>(`/admin/billing/webhook-events${qs ? `?${qs}` : ''}`)
    },
    reconcileUser: (userId: string) =>
      apiPost<AdminBillingReconcileResult>(`/admin/billing/users/${encodeURIComponent(userId)}/reconcile`, {}),
  },
}

export interface AdminBillingConfigStatus {
  stripe_configured: boolean
  webhook_configured: boolean
  insecure_webhook_allowed: boolean
}

export interface AdminBillingSummary {
  config: AdminBillingConfigStatus
  plan_counts: { plan: string; count: number }[]
}

export interface AdminUserBillingRow {
  user_id: string
  email: string
  plan: string
  stripe_customer_id?: string
  stripe_subscription_id?: string
  stripe_subscription_status?: string
  stripe_price_id?: string
  cancel_at_period_end?: boolean
  period_start?: string
  period_end?: string
  allowance_credits: number
  period_used_credits: number
  topup_credits_remaining: number
  trial_remaining_credits: number
}

export interface AdminWebhookEventRow {
  event_id: string
  event_type: string
  status: string
  attempt_count: number
  user_id?: string
  stripe_customer_id?: string
  error_code?: string
  error_message?: string
  received_at: string
  processed_at?: string
}

export interface AdminBillingReconcileResult {
  user_id: string
  stripe_customer_id?: string
  stripe_subscription_id?: string
  stripe_subscription_status?: string
  action: string
  message?: string
}
