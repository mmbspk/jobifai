import { apiGet, apiPost, apiPostForm, apiPut, apiDelete } from './client'
import type { GeneralSettings, WorkPreferences, ResumeProfile, SecretsConfig, ResumeStyle, ResumeMarket, AIProviderStatus } from '../types'

export interface AIProviderTestResult {
  success: boolean
  latency_ms: number
  provider: string
  model: string
  error?: string
}

export const settingsApi = {
  general: {
    get: () => apiGet<GeneralSettings>('/settings/general'),
    set: (body: GeneralSettings) => apiPost<void>('/settings/general', body),
  },
  preferences: {
    get: () => apiGet<WorkPreferences>('/settings/preferences'),
    set: (body: WorkPreferences) => apiPost<void>('/settings/preferences', body),
  },
  resume: {
    get: () => apiGet<ResumeProfile>('/settings/resume'),
    set: (body: ResumeProfile) => apiPost<void>('/settings/resume', body),
    upload: (file: File) => {
      const form = new FormData()
      form.set('resume_file', file)
      return apiPostForm<ResumeProfile>('/settings/resume/upload', form)
    },
    downloadUrl: () => '/api/settings/resume/download',
  },
  secrets: {
    get: () => apiGet<SecretsConfig>('/settings/secrets'),
    setApiKey: (keyType: string, value: string) => apiPost<void>('/settings/secrets/api-key', { key_type: keyType, value }),
    deleteApiKey: () => apiDelete<void>('/settings/secrets/api-key'),
    setCredentials: (platform: string, email: string, password: string) =>
      apiPost<void>('/settings/secrets/credentials', { platform, email, password }),
    deleteCredentials: (platform: string) =>
      apiDelete<void>(`/settings/secrets/credentials?platform=${encodeURIComponent(platform)}`),
  },
  aiProvider: {
    get: () => apiGet<AIProviderStatus>('/settings/ai-provider'),
    set: (cfg: { provider: string; model: string }) => apiPut<void>('/settings/ai-provider', cfg),
    test: () => apiPost<AIProviderTestResult>('/settings/ai-provider/test', {}),
  },
  styles: {
    list: () => apiGet<ResumeStyle[]>('/settings/styles'),
  },
  markets: {
    list: () => apiGet<ResumeMarket[]>('/settings/markets'),
  },
  locations: {
    suggest: (q: string) => apiGet<string[]>(`/settings/locations/suggest?q=${encodeURIComponent(q)}`),
  },
}
