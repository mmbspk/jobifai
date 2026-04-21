import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { userApi, type AuthTokens, type Me } from '../api/user'
import { apiGet, apiPut } from '../api/client'

interface AuthState {
  user: Me | null
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, password: string, displayName?: string) => Promise<void>
  logout: () => Promise<void>
  updateMe: (displayName: string, avatarUrl?: string) => Promise<void>
  googleLoginUrl: string
}

const AuthContext = createContext<AuthState | null>(null)

const ACCESS_KEY = 'access_token'
const REFRESH_KEY = 'refresh_token'

function saveTokens(tokens: AuthTokens) {
  localStorage.setItem(ACCESS_KEY, tokens.access_token)
  localStorage.setItem(REFRESH_KEY, tokens.refresh_token)
}

function clearTokens() {
  localStorage.removeItem(ACCESS_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<Me | null>(null)
  const [loading, setLoading] = useState(true)
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const scheduleRefresh = useCallback((expiresIn: number) => {
    if (refreshTimer.current) clearTimeout(refreshTimer.current)
    const delay = Math.max((expiresIn - 300) * 1000, 60_000) // refresh 5 min early
    refreshTimer.current = setTimeout(async () => {
      const raw = localStorage.getItem(REFRESH_KEY)
      if (!raw) return
      try {
        const tokens = await userApi.refresh(raw)
        saveTokens(tokens)
        scheduleRefresh(tokens.expires_in)
      } catch {
        clearTokens()
        setUser(null)
      }
    }, delay)
  }, [])

  // On mount: if we have an access token, fetch /api/me to hydrate user state.
  useEffect(() => {
    const token = localStorage.getItem(ACCESS_KEY)
    if (!token) {
      setLoading(false)
      return
    }
    apiGet<Me>('/me')
      .then(me => {
        setUser(me)
        // Schedule a refresh; we don't know exact expiry so use 23h (near 24h TTL).
        scheduleRefresh(23 * 3600)
      })
      .catch(() => {
        // Token invalid/expired — try refresh.
        const raw = localStorage.getItem(REFRESH_KEY)
        if (!raw) { clearTokens(); setLoading(false); return }
        userApi.refresh(raw)
          .then(tokens => {
            saveTokens(tokens)
            scheduleRefresh(tokens.expires_in)
            return apiGet<Me>('/me')
          })
          .then(me => setUser(me))
          .catch(() => { clearTokens(); setUser(null) })
          .finally(() => setLoading(false))
        return
      })
      .finally(() => setLoading(false))
  }, [scheduleRefresh])

  // Handle Google OAuth redirect: /?access_token=...&refresh_token=...
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const at = params.get('access_token')
    const rt = params.get('refresh_token')
    if (at && rt) {
      saveTokens({ access_token: at, refresh_token: rt, expires_in: 86400 })
      scheduleRefresh(86400)
      // Clean up URL without triggering a reload.
      window.history.replaceState({}, '', '/')
      apiGet<Me>('/me').then(me => setUser(me)).catch(() => {})
    }
  }, [scheduleRefresh])

  const login = useCallback(async (email: string, password: string) => {
    const tokens = await userApi.login(email, password)
    saveTokens(tokens)
    scheduleRefresh(tokens.expires_in)
    const me = await apiGet<Me>('/me')
    setUser(me)
  }, [scheduleRefresh])

  const register = useCallback(async (email: string, password: string, displayName?: string) => {
    const tokens = await userApi.register(email, password, displayName)
    saveTokens(tokens)
    scheduleRefresh(tokens.expires_in)
    const me = await apiGet<Me>('/me')
    setUser(me)
  }, [scheduleRefresh])

  const logout = useCallback(async () => {
    const raw = localStorage.getItem(REFRESH_KEY)
    if (raw) { try { await userApi.logout(raw) } catch { /* best effort */ } }
    clearTokens()
    if (refreshTimer.current) clearTimeout(refreshTimer.current)
    setUser(null)
  }, [])

  const updateMe = useCallback(async (displayName: string, avatarUrl = '') => {
    await apiPut('/me', { display_name: displayName, avatar_url: avatarUrl })
    setUser(prev => prev ? { ...prev, display_name: displayName, avatar_url: avatarUrl } : prev)
  }, [])

  return (
    <AuthContext.Provider value={{
      user,
      loading,
      login,
      register,
      logout,
      updateMe,
      googleLoginUrl: userApi.googleLoginUrl(),
    }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}
