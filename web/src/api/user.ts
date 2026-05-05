// Auth API, public endpoints (no Bearer header required)
const BASE = (import.meta.env.VITE_API_BASE ?? '')

async function authFetch<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const j = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(j.message ?? res.statusText)
  return j as T
}

export interface AuthTokens {
  access_token: string
  refresh_token: string
  expires_in: number
}

export interface Me {
  id: string
  email: string
  display_name: string
  avatar_url: string
  has_password: boolean
  has_google: boolean
}

export const userApi = {
  register: (email: string, password: string, displayName?: string) =>
    authFetch<AuthTokens>('/auth/register', { email, password, display_name: displayName ?? '' }),

  login: (email: string, password: string) =>
    authFetch<AuthTokens>('/auth/login', { email, password }),

  refresh: (refreshToken: string) =>
    authFetch<AuthTokens>('/auth/refresh', { refresh_token: refreshToken }),

  logout: (refreshToken: string) =>
    authFetch<void>('/auth/logout', { refresh_token: refreshToken }),

  googleLoginUrl: () => `${BASE}/auth/google`,
}
