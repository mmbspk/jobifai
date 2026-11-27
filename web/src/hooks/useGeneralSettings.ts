import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'
import { useAuth } from '../contexts/AuthContext'

export const generalSettingsQueryKey = (userId: string | undefined) => ['settings-general', userId] as const

// Shared persisted, effective settings. Account-scoped keys keep an old
// account's settings and late responses out of the current account's UI.
export function useGeneralSettings(staleTime = 30_000) {
  const { user, loading } = useAuth()
  const queryKey = generalSettingsQueryKey(user?.id)
  const query = useQuery({
    queryKey,
    queryFn: settingsApi.general.get,
    enabled: !loading && !!user?.id,
    staleTime,
  })
  return { ...query, queryKey }
}
