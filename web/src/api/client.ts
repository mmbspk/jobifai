// Base API client, reads VITE_API_BASE for native/remote builds
const BASE = (import.meta.env.VITE_API_BASE ?? '') + '/api'
const WS_BASE = (import.meta.env.VITE_WS_BASE ?? (location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + location.host)

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, message: string, code = '') {
    super(message)
    this.status = status
    this.code = code
  }
}

export type ApiRequestOpts = {
  signal?: AbortSignal
  timeoutMs?: number
}

function mergeSignals(opts?: number | ApiRequestOpts): AbortSignal | undefined {
  const o: ApiRequestOpts = typeof opts === 'number' ? { timeoutMs: opts } : opts ?? {}
  let signal = o.signal
  if (o.timeoutMs && o.timeoutMs > 0) {
    const timeout = AbortSignal.timeout(o.timeoutMs)
    signal = signal ? AbortSignal.any([signal, timeout]) : timeout
  }
  return signal
}

export function isAbortError(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError'
}

export function getToken(): string | null {
  return localStorage.getItem('access_token')
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken()
  const authHeader: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...authHeader, ...init?.headers },
  })
  if (res.ok) {
    if (res.status === 204) return undefined as T
    return res.json()
  }
  let msg = res.statusText
  let code = ''
  try { const j = await res.json(); msg = j.error ?? j.message ?? msg; code = j.code ?? '' } catch { /* ignore */ }
  throw new ApiError(res.status, msg, code)
}

export async function apiGet<T>(path: string): Promise<T> {
  return apiFetch<T>(path, { method: 'GET', headers: {} })
}

export async function apiPost<T>(path: string, body?: unknown, opts?: number | ApiRequestOpts): Promise<T> {
  const init: RequestInit = {
    method: 'POST',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  }
  const signal = mergeSignals(opts)
  if (signal) init.signal = signal
  try {
    return await apiFetch<T>(path, init)
  } catch (e: unknown) {
    if (isAbortError(e)) throw e
    if (e instanceof DOMException && e.name === 'TimeoutError') {
      throw new ApiError(504, 'Request timed out — try again')
    }
    throw e
  }
}

export async function apiPut<T>(path: string, body?: unknown): Promise<T> {
  return apiFetch<T>(path, {
    method: 'PUT',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
}

export async function apiDelete<T>(path: string): Promise<T> {
  return apiFetch<T>(path, { method: 'DELETE', headers: {} })
}

export async function apiPostForm<T>(path: string, form: FormData, opts?: ApiRequestOpts): Promise<T> {
  const token = getToken()
  const authHeader: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
  const signal = mergeSignals(opts)
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    body: form,
    headers: authHeader,
    signal,
  })
  if (!res.ok) {
    let msg = res.statusText
    let code = ''
    try { const j = await res.json(); msg = j.error ?? j.message ?? msg; code = j.code ?? '' } catch { /* ignore */ }
    throw new ApiError(res.status, msg, code)
  }
  // PDF responses
  const ct = res.headers.get('Content-Type') ?? ''
  if (ct.includes('application/pdf')) return res.blob() as unknown as T
  if (res.status === 204) return undefined as T
  return res.json()
}

export function wsUrl(path: string): string {
  return `${WS_BASE}${path}`
}
