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

  // On mount: restore session from OAuth redirect params or localStorage.
  useEffect(() => {
    // Check for Google OAuth redirect tokens first to avoid a race where
    // loading becomes false before the URL params are consumed, causing
    // PrivateRoute to redirect to /login prematurely.
    const params = new URLSearchParams(window.location.search)
    const oauthAt = params.get('access_token')
    const oauthRt = params.get('refresh_token')
    if (oauthAt && oauthRt) {
      saveTokens({ access_token: oauthAt, refresh_token: oauthRt, expires_in: 86400 })
      scheduleRefresh(86400)
      window.history.replaceState({}, '', '/')
      apiGet<Me>('/me')
        .then(me => setUser(me))
        .catch(() => clearTokens())
        .finally(() => setLoading(false))
      return
    }

    const token = localStorage.getItem(ACCESS_KEY)
    if (!token) {
      setLoading(false)
      return
    }
    apiGet<Me>('/me')
      .then(me => {
        setUser(me)
        scheduleRefresh(23 * 3600)
      })
      .catch(() => {
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
