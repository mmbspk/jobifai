import { apiGet, apiPost, apiDelete } from './client'
import type { PlatformSession } from '../types'

export const authApi = {
  platformStatus: (platform: string) => apiGet<PlatformSession>(`/auth/${platform}/status`),
  launchBrowser: (platform: string, useProfile = false, profilePath?: string) =>
    apiPost<{ session_id: string; message: string }>('/auth/launch-browser', {
      platform,
      use_profile: useProfile,
      profile_path: profilePath,
    }),
  saveSession: (sessionId: string, platform: string) =>
    apiPost<void>('/auth/save-session', { session_id: sessionId, platform }),
  deleteSession: (platform: string) => apiDelete<void>(`/auth/${platform}/session`),
}
