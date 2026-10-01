import { useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../../contexts/AuthContext'
import { consumePostAuthRedirect } from '../../lib/postAuthRedirect'

/** Paths where a stored redirect may run (OAuth lands on `/`; sign-in pages may still hold a saved target). */
const POST_AUTH_APPLY_PATHS = new Set(['/', '/login', '/register'])

/** Applies a stored post-OAuth redirect once the session is active. */
export function PostAuthRedirect() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const applied = useRef(false)

  useEffect(() => {
    if (!user || applied.current) return
    if (!POST_AUTH_APPLY_PATHS.has(location.pathname)) return
    const dest = consumePostAuthRedirect()
    if (!dest) return
    const here = `${location.pathname}${location.search}${location.hash}`
    if (dest === here) return
    applied.current = true
    navigate(dest, { replace: true })
  }, [user, navigate, location.pathname, location.search, location.hash])

  return null
}
