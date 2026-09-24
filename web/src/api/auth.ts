import { apiGet, apiPost, apiDelete } from './client'
import type { PlatformSession } from '../types'

export const authApi = {
  platformStatus: (platform: string) => apiGet<PlatformSession>(`/auth/${platform}/status`),
  browserPending: (platform: string) =>
    apiGet<{ pending: boolean; session_id?: string }>(`/auth/${platform}/browser-pending`),
  launchBrowser: (platform: string, opts?: { force?: boolean; useProfile?: boolean; profilePath?: string }) =>
    apiPost<{ session_id: string; message: string }>('/auth/launch-browser', {
      platform,
      use_profile: opts?.useProfile ?? false,
      profile_path: opts?.profilePath,
      force: opts?.force ?? false,
    }),
  saveSession: (sessionId: string, platform: string) =>
    apiPost<void>('/auth/save-session', { session_id: sessionId, platform }),
  deleteSession: (platform: string) => apiDelete<void>(`/auth/${platform}/session`),
}
