import { apiDelete, apiGet, apiPost, apiPut } from './client'
import type { AdminUserDetail, AdminUserRow, GeneralSettings, LLMOverrides } from '../types'

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
    get: (userId: string) => apiGet<AdminUserDetail>(`/admin/users/${userId}`),
    update: (userId: string, body: { is_admin?: boolean; llm_overrides?: LLMOverrides }) =>
      apiPut<void>(`/admin/users/${userId}`, body),
    setApiKey: (userId: string, value: string) =>
      apiPost<void>(`/admin/users/${userId}/secrets/api-key`, { value }),
    deleteApiKey: (userId: string) =>
      apiDelete(`/admin/users/${userId}/secrets/api-key`),
  },
}
