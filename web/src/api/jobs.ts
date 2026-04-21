import { apiGet, apiPost, apiDelete } from './client'
import type { AppliedJob, SkippedJob, JobStats, PendingReview } from '../types'

export interface JobsQuery {
  platform?: string
  skip_reason?: string
  limit?: number
  offset?: number
}

function qs(q: JobsQuery): string {
  const p = new URLSearchParams()
  if (q.platform) p.set('platform', q.platform)
  if (q.skip_reason) p.set('skip_reason', q.skip_reason)
  if (q.limit != null) p.set('limit', String(q.limit))
  if (q.offset != null) p.set('offset', String(q.offset))
  const s = p.toString()
  return s ? `?${s}` : ''
}

export const jobsApi = {
  applied: (q: JobsQuery = {}) => apiGet<AppliedJob[]>(`/jobs/applied${qs(q)}`),
  deleteApplied: (id: string) => apiDelete<{ status: string }>(`/jobs/applied/${id}`),
  skipped: (q: JobsQuery = {}) => apiGet<SkippedJob[]>(`/jobs/skipped${qs(q)}`),
  deleteSkipped: (id: string) => apiDelete<{ status: string }>(`/jobs/skipped/${id}`),
  cannotApply: (q: JobsQuery = {}) => apiGet<SkippedJob[]>(`/jobs/cannot-apply${qs(q)}`),
  requeueCannotApply: (id: string) => apiPost<{ status: string }>(`/jobs/cannot-apply/${id}/requeue`),
  topMatches: (q: JobsQuery = {}) => apiGet<PendingReview[]>(`/jobs/top-matches${qs(q)}`),
  deletePendingReview: (id: string) => apiDelete<{ status: string }>(`/jobs/pending-review/${id}`),
  markApplied: (id: string) => apiPost<{ status: string }>(`/jobs/pending-review/${id}/mark-applied`),
  stats: () => apiGet<JobStats>(`/jobs/stats`),
  get: (id: string) => apiGet<{ id: string; status: string; applied_job?: AppliedJob; skipped_job?: SkippedJob }>(`/jobs/${id}`),
}
