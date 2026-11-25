import { useInfiniteQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

export function useApplicationAnswerReviews(enabled = true, state: 'active' | 'history' = 'active') {
  return useInfiniteQuery({
    queryKey: state === 'active' ? ['application-answer-reviews'] : ['application-answer-reviews', 'history'],
    queryFn: ({ pageParam }) => settingsApi.applicationAnswers.reviews(pageParam, state),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: page => page.next_cursor || undefined,
    refetchInterval: 30_000,
    enabled,
  })
}
