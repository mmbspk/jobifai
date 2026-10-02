const ACCESS_KEY = 'access_token'
const REFRESH_KEY = 'refresh_token'

/** True when this browser has previously signed in (tokens may be expired). */
export function hasStoredAuthCredentials(): boolean {
  return Boolean(localStorage.getItem(ACCESS_KEY) || localStorage.getItem(REFRESH_KEY))
}

export const authStorageKeys = { access: ACCESS_KEY, refresh: REFRESH_KEY } as const
