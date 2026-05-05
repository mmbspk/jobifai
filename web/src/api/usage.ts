import { apiGet } from './client'
import type { SessionUsage } from '../types'

export const usageApi = {
  session: () => apiGet<SessionUsage>('/usage/session'),
  totals: () => apiGet<SessionUsage>('/usage/totals'),
}
