import { apiGet, apiPost, apiPostForm, apiPut, apiPatch, apiDelete } from './client'
import type { GeneralSettings, WorkPreferences, ResumeProfile, SecretsConfig, ResumeStyle, ResumeMarket, AIProviderStatus, ApplicationDefaults, Certification } from '../types'

export interface AIProviderTestResult {
  success: boolean
  latency_ms: number
  provider: string
  model: string
  error?: string
}

export interface CVAnswerPrefill {
  topic_id: string
  binding: string
  value: unknown
  source: string
  review_state: string
  needs_confirmation?: boolean
  warning?: string
  evidence: { quote: string; start: number; end: number; page?: number }[]
}

export type ResumeSettingsProfile = ResumeProfile & { profile_revision?: string }
export type ResumeUploadResponse = ResumeProfile & { application_answer_prefill?: CVAnswerPrefill[]; profile_revision?: string; import_id?: string }

export interface ApplicationAnswerSource {
  source: 'cv' | 'profile' | 'preferences' | 'candidate' | 'confirmed'
  label?: string
  evidence?: string[]
}

export interface ApplicationAnswerTopic {
  id: string
  label: string
  core: boolean
  summary?: string
}

export interface ApplicationAnswersResponse {
  application_defaults: ApplicationDefaults
  revision: string
  profile_revision?: string
  sources: Record<string, ApplicationAnswerSource>
  catalog: ApplicationAnswerTopic[]
  profile?: ResumeProfile
  preferences?: WorkPreferences
  certifications?: Certification[]
}

export interface ApplicationAnswerReview {
  id: string
  question: string
  status: 'used' | 'suggested' | 'required'
  answer_used?: string
  suggested_answer?: string
  revision: number
  topic_id?: string
  country?: string
  employer?: string
  platform?: string
  job_title?: string
  job_id?: string
  profile_revision?: string
  use_status?: 'not_used' | 'filled' | 'replaced' | 'submitted' | 'submission_unknown'
  lifecycle?: string
  evidence?: string[]
  occurrence_count?: number
  created_at?: string
}

export interface ApplicationAnswerReviewsResponse {
  items: ApplicationAnswerReview[]
  unresolved_count: number
  unread_count: number
  next_cursor?: string
}

export const settingsApi = {
  applicationAnswers: {
    get: () => apiGet<ApplicationAnswersResponse>('/settings/application-answers'),
    set: (body: { topics: Record<string, unknown>; revision: string }) => apiPatch<ApplicationAnswersResponse>('/settings/application-answers', body),
    reviews: (cursor?: string, state: 'active' | 'history' = 'active') => apiGet<ApplicationAnswerReviewsResponse>('/settings/application-answers/reviews' + (cursor || state === 'history' ? '?' + [cursor ? 'cursor=' + encodeURIComponent(cursor) : '', state === 'history' ? 'state=history' : ''].filter(Boolean).join('&') : '')),
    review: (id: string) => apiGet<ApplicationAnswerReview>('/settings/application-answers/reviews/' + encodeURIComponent(id)),
    confirm: (id: string, body: { answer: string; revision: number; profile_revision?: string }) => apiPost<{ value: ApplicationDefaults; revision: string }>('/settings/application-answers/reviews/' + encodeURIComponent(id) + '/confirm', body),
    dismiss: (id: string, revision: number) => apiPost<void>('/settings/application-answers/reviews/' + encodeURIComponent(id) + '/dismiss', { revision }),
    markRead: () => apiPost<void>('/settings/application-answers/reviews/read', {}),
  },
  formMappings: {
    list: () => apiGet<{ entries: FormMappingReview[] }>('/settings/form-mappings'),
    confirm: (id: string, body: { confirmed: true; binding_key?: string; answer?: string }) => apiPost<void>('/settings/form-mappings/' + encodeURIComponent(id) + '/confirm', body),
    invalidate: (id: string) => apiDelete<void>('/settings/form-mappings/' + encodeURIComponent(id)),
  },
  general: {
    get: () => apiGet<GeneralSettings>('/settings/general'),
    set: (body: GeneralSettings) => apiPost<void>('/settings/general', body),
  },
  preferences: {
    get: () => apiGet<WorkPreferences>('/settings/preferences'),
    set: (body: WorkPreferences) => apiPost<void>('/settings/preferences', body),
  },
  resume: {
    get: () => apiGet<ResumeSettingsProfile>('/settings/resume'),
    set: (body: ResumeSettingsProfile & { import_id?: string }) => apiPost<void>('/settings/resume', body),
    upload: (file: File) => {
      const form = new FormData()
      form.set('resume_file', file)
      return apiPostForm<ResumeUploadResponse>('/settings/resume/upload', form)
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

export interface FormMappingReview {
  id: string
  field: { label: string; group?: string; help_text?: string; type: string; options?: string[]; job_title?: string; job_location?: string; max_chars?: number; min_value?: string; max_value?: string; step_value?: string; pattern?: string; locale?: string; input_type?: string }
  platform: string
  employer_key: string
  binding_key: string
  provenance: string
}
