import { apiGet } from './client'
import type { SessionUsage, UsageSummary } from '../types'

export const usageApi = {
  session: () => apiGet<SessionUsage>('/usage/session'),
  totals: () => apiGet<SessionUsage>('/usage/totals'),
  summary: () => apiGet<UsageSummary>('/usage/summary'),
}

