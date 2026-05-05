import { apiGet, apiPost, apiPostForm } from './client'
import type { GeneralSettings, WorkPreferences, ResumeProfile, SecretsConfig, ResumeStyle, ResumeMarket } from '../types'

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
    setCredentials: (platform: string, email: string, password: string) =>
      apiPost<void>('/settings/secrets/credentials', { platform, email, password }),
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
