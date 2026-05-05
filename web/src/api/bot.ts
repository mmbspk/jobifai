import { apiGet, apiPost } from './client'
import type { BotStatus, PendingReview } from '../types'

export const botApi = {
  status: () => apiGet<BotStatus>('/bot/status'),
  start: (platform: string) => apiPost<void>('/bot/start', { platform }),
  stop: () => apiPost<void>('/bot/stop'),
  pause: () => apiPost<void>('/bot/pause'),
  resume: () => apiPost<void>('/bot/resume'),
  reviewPending: () => apiGet<PendingReview[]>('/bot/review/pending'),
  reviewApprove: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/approve`),
  reviewReject: (jobId: string) => apiPost<void>(`/bot/review/${jobId}/reject`),
}
