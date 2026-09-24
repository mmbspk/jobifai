import { useQuery, useQueryClient } from '@tanstack/react-query'
import { quotaApi } from '../api/quota'

export const QUOTA_QUERY_KEY = ['quota-status'] as const

export function useQuota() {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: QUOTA_QUERY_KEY,
    queryFn: quotaApi.status,
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
  })
  const blocked = Boolean(q.data?.blocked)
  const unlimited = Boolean(q.data?.unlimited)
  const aiDisabled = blocked && !unlimited

  const refreshQuota = () => qc.invalidateQueries({ queryKey: QUOTA_QUERY_KEY })

  return {
    ...q,
    blocked,
    unlimited,
    aiDisabled,
    refreshQuota,
  }
}

/** Poll quota after Stripe redirect until webhook applies or attempts exhausted. */
export async function syncQuotaAfterPayment(
  refresh: () => Promise<unknown>,
  opts?: { attempts?: number; intervalMs?: number },
): Promise<void> {
  const attempts = opts?.attempts ?? 12
  const intervalMs = opts?.intervalMs ?? 2500
  for (let i = 0; i < attempts; i++) {
    await refresh()
    if (i < attempts - 1) {
      await new Promise<void>(resolve => {
        setTimeout(resolve, intervalMs)
      })
    }
  }
}
