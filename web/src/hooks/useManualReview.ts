import { useQuery, type UseQueryOptions } from '@tanstack/react-query'
import { useAuth } from '../contexts/AuthContext'
import { botApi } from '../api/bot'
import type { PendingReview } from '../types'
import { useGeneralSettings } from './useGeneralSettings'

export function useManualReview() {
  const { user, loading } = useAuth()
  const settings = useGeneralSettings()
  return {
    enabled: !loading && !!user?.id && settings.data?.require_review_before_submit === true,
    loading: loading || (settings.data === undefined && settings.isPending),
    isError: settings.data === undefined && settings.isError,
    refetch: settings.refetch,
  }
}

export const reviewPendingQueryKey = (userId: string | undefined) => ['review-pending', userId] as const

export function usePendingReviews(refetchInterval: UseQueryOptions<PendingReview[]>['refetchInterval'] = 30_000) {
  const { user } = useAuth()
  const review = useManualReview()
  const query = useQuery({
    queryKey: reviewPendingQueryKey(user?.id),
    queryFn: botApi.reviewPending,
    enabled: review.enabled,
    staleTime: 30_000,
    refetchInterval: review.enabled ? refetchInterval : false,
  })
  // A disabled observer can still contain cached data. Preserve it for
  // re-enabling review, but never expose it through hidden queue UI.
  return { ...query, data: review.enabled ? query.data : undefined, isLoading: review.enabled && query.isLoading }
}
