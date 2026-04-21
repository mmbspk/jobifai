import { apiGet } from './client'
import type { SessionUsage } from '../types'

export const usageApi = {
  session: () => apiGet<SessionUsage>('/usage/session'),
}
