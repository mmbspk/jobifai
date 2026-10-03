import { apiGet, apiPost, apiPut, type ApiRequestOpts } from './client'
import type { BotStatus, PendingReview } from '../types'

export interface ApplyURLResponse {
  status: 'applied' | 'score_warning' | 'already_applied' | 'not_easy_apply'
  message?: string
  score?: number
  reasoning?: string
  job_id?: string
  company?: string
  role?: string
}

export interface ReviewDocumentChoice {
  version_id: string
  document_id: string
  kind: string
  title: string
  source?: string
  is_original?: boolean
  has_pdf: boolean
  reconstructible?: boolean
}

export interface ReviewDocumentAction {
  kind: string
  mode?: string
  action: string
  uses_ai: boolean
  credits_estimate?: number | null
  hold_reason?: string
  version_id?: string
}

export interface ReviewDocumentsResponse {
  options: { resume: ReviewDocumentChoice[]; cover: ReviewDocumentChoice[] }
  preflight: {
    policies: { resume_mode?: string; cover_mode?: string; fallback?: Record<string, boolean> }
    fallbacks: Record<string, boolean>
    selection: {
      resume_version_id?: string
      cover_version_id?: string
      resume_use_site?: boolean
      cover_skip?: boolean
    }
    capabilities: Record<string, unknown>
    capabilities_known: boolean
    prepared: boolean
    resume: ReviewDocumentAction
    cover: ReviewDocumentAction
    blocking_reasons?: string[]
    pack_hold_reason?: string
  }
}

export const botApi = {
  status: () => apiGet<BotStatus>('/bot/status'),
  start: (platform: string) => apiPost<void>('/bot/start', { platform }),
  stop: () => apiPost<void>('/bot/stop'),
  pause: () => apiPost<void>('/bot/pause'),
  resume: () => apiPost<void>('/bot/resume'),
  reviewPending: () => apiGet<PendingReview[]>('/bot/review/pending'),
  reviewDocuments: (jobId: string) => apiGet<ReviewDocumentsResponse>(`/bot/review/${jobId}/documents`),
  reviewDocumentsPut: (
    jobId: string,
    body: {
      resume_version_id?: string
      cover_version_id?: string
      resume_use_site?: boolean
      cover_skip?: boolean
    },
  ) => apiPut<ReviewDocumentsResponse>(`/bot/review/${jobId}/documents`, body),
  reviewPrepare: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/prepare`, undefined, { timeoutMs: 4.5 * 60 * 1000 }),
  reviewApprove: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/approve`, undefined, { timeoutMs: 4.5 * 60 * 1000 }),
  reviewReject: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/reject`),
  applyFromURL: (url: string, market: string, force = false, opts?: Pick<ApiRequestOpts, 'signal'>) =>
    apiPost<ApplyURLResponse>('/bot/apply-url', { url, market, force }, {
      timeoutMs: 4.5 * 60 * 1000,
      signal: opts?.signal,
    }),
}
