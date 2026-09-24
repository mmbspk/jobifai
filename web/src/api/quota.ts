import { apiGet, apiPut } from './client'
import type { QuotaDefaults, QuotaStatus } from '../types'

export const quotaApi = {
  status: () => apiGet<QuotaStatus>('/quota/status'),
  adminDefaults: {
    get: () => apiGet<QuotaDefaults>('/admin/quota/defaults'),
    set: (body: QuotaDefaults) => apiPut<void>('/admin/quota/defaults', body),
  },
}
