import { apiGet, apiPost } from './client'
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

export const botApi = {
  status: () => apiGet<BotStatus>('/bot/status'),
  start: (platform: string) => apiPost<void>('/bot/start', { platform }),
  stop: () => apiPost<void>('/bot/stop'),
  pause: () => apiPost<void>('/bot/pause'),
  resume: () => apiPost<void>('/bot/resume'),
  reviewPending: () => apiGet<PendingReview[]>('/bot/review/pending'),
  reviewApprove: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/approve`),
  reviewReject: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/reject`),
  applyFromURL: (url: string, market: string, force = false) =>
    apiPost<ApplyURLResponse>('/bot/apply-url', { url, market, force }, 4.5 * 60 * 1000),
}
