import { apiDeleteWithBody, apiPost, apiPut } from './client'
import { authStorageKeys } from '../lib/authSession'

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
  is_admin?: boolean
  email_verified?: boolean
  pending_email?: string | null
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

  resendVerification: (email: string) =>
    authFetch<{ message: string }>('/auth/resend-verification', { email }),

  forgotPassword: (email: string) =>
    authFetch<{ message: string }>('/auth/forgot-password', { email }),

  resetPassword: (token: string, newPassword: string) =>
    authFetch<{ message: string }>('/auth/reset-password', { token, new_password: newPassword }),

  verifyEmail: async (token: string): Promise<{ message: string }> => {
    const res = await fetch(`${BASE}/auth/verify-email?token=${encodeURIComponent(token)}`)
    const j = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(j.message ?? res.statusText)
    return j as { message: string }
  },

  // Authenticated — uses Bearer token from localStorage via apiPut.
  // /api prefix is added by client.ts, so path here is /me/password.
  // Sends the current refresh token so the backend can preserve this session
  // while revoking all other active sessions.
  changePassword: (currentPassword: string, newPassword: string) =>
    apiPut<{ message: string }>('/me/password', {
      current_password: currentPassword,
      new_password: newPassword,
      refresh_token: localStorage.getItem(authStorageKeys.refresh) ?? '',
    }),

  requestEmailChange: (newEmail: string, currentPassword: string) =>
    apiPost<{ message: string }>('/me/email', {
      new_email: newEmail,
      current_password: currentPassword,
    }),

  verifyEmailChange: async (token: string): Promise<{ message: string; code: string }> => {
    const res = await fetch(`${BASE}/auth/verify-email-change?token=${encodeURIComponent(token)}`)
    const j = await res.json().catch(() => ({}))
    if (!res.ok) throw Object.assign(new Error(j.message ?? res.statusText), { code: j.code ?? '' })
    return j as { message: string; code: string }
  },

  deleteAccount: (opts: { currentPassword?: string; confirm?: string }) =>
    apiDeleteWithBody<{ message: string }>('/me', {
      current_password: opts.currentPassword ?? '',
      confirm: opts.confirm ?? '',
    }),
}
