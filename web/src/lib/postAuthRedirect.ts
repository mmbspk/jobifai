const STORAGE_KEY = 'jobifai:post_auth_redirect'

const BLOCKED_PATHS = new Set(['/login', '/register', '/welcome', '/pricing'])

/** Safe in-app path for redirect after sign-in; default `/` (dashboard home). */
export function sanitizePostAuthPath(path: string | null | undefined): string | null {
  if (!path || !path.startsWith('/') || path.startsWith('//')) return null
  const pathname = path.split(/[?#]/)[0]
  if (BLOCKED_PATHS.has(pathname)) return null
  return path
}

export function postAuthPathFromLocation(
  from: { pathname: string; search?: string; hash?: string } | undefined,
): string {
  if (!from?.pathname) return '/'
  const combined = `${from.pathname}${from.search ?? ''}${from.hash ?? ''}`
  return sanitizePostAuthPath(combined) ?? '/'
}

export function savePostAuthRedirect(path: string): void {
  const safe = sanitizePostAuthPath(path)
  if (safe) sessionStorage.setItem(STORAGE_KEY, safe)
}

export function peekPostAuthRedirect(): string | null {
  return sanitizePostAuthPath(sessionStorage.getItem(STORAGE_KEY))
}

export function clearPostAuthRedirect(): void {
  sessionStorage.removeItem(STORAGE_KEY)
}

export function consumePostAuthRedirect(): string | null {
  const dest = peekPostAuthRedirect()
  clearPostAuthRedirect()
  return dest
}

/** Path to open after sign-in: router state first, then session fallback (protected-route redirect). */
export function resolvePostAuthDestination(
  from: { pathname: string; search?: string; hash?: string } | undefined,
): string {
  if (from?.pathname) {
    const combined = `${from.pathname}${from.search ?? ''}${from.hash ?? ''}`
    const safe = sanitizePostAuthPath(combined)
    if (safe) return safe
  }
  return peekPostAuthRedirect() ?? '/'
}
