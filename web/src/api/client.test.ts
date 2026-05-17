import { describe, test, expect, vi, beforeEach } from 'vitest'

// client.ts reads localStorage at call time (getToken). jsdom's localStorage
// isn't available before modules load in this vitest setup, so install a
// working stub via vi.hoisted before any module-level code runs.
vi.hoisted(() => {
  const store: Record<string, string> = {}
  ;(globalThis as Record<string, unknown>).localStorage = {
    getItem: (k: string): string | null => store[k] ?? null,
    setItem: (k: string, v: string): void => { store[k] = v },
    removeItem: (k: string): void => { delete store[k] },
    clear: (): void => { Object.keys(store).forEach(k => { delete store[k] }) },
    key: (): null => null,
    length: 0,
  }
})

import { ApiError, getToken, apiGet, apiPost, apiDelete } from './client'

beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

// ── ApiError ──────────────────────────────────────────────────────────────────

describe('ApiError', () => {
  test('is an instance of Error', () => {
    expect(new ApiError(404, 'not found')).toBeInstanceOf(Error)
  })

  test('carries status and message', () => {
    const e = new ApiError(401, 'unauthorized')
    expect(e.status).toBe(401)
    expect(e.message).toBe('unauthorized')
  })

  test('carries optional code (defaults to empty string)', () => {
    expect(new ApiError(403, 'forbidden').code).toBe('')
    expect(new ApiError(403, 'forbidden', 'FORBIDDEN').code).toBe('FORBIDDEN')
  })
})

// ── getToken ──────────────────────────────────────────────────────────────────

describe('getToken', () => {
  test('returns null when no token stored', () => {
    expect(getToken()).toBeNull()
  })

  test('returns the stored access token', () => {
    localStorage.setItem('access_token', 'tok.abc')
    expect(getToken()).toBe('tok.abc')
  })
})

// ── apiGet ────────────────────────────────────────────────────────────────────

describe('apiGet', () => {
  test('returns parsed JSON on 200', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ hello: 'world' }),
    } as Response)

    const result = await apiGet<{ hello: string }>('/test')
    expect(result).toEqual({ hello: 'world' })
  })

  test('throws ApiError on non-200 with server message', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: async () => ({ message: 'resource not found' }),
    } as Response)

    await expect(apiGet('/missing')).rejects.toMatchObject({
      status: 404,
      message: 'resource not found',
    })
  })

  test('throws ApiError using statusText when body has no message', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => { throw new Error('not json') },
    } as unknown as Response)

    await expect(apiGet('/boom')).rejects.toMatchObject({
      status: 500,
      message: 'Internal Server Error',
    })
  })

  test('includes Authorization header when a token is stored', async () => {
    localStorage.setItem('access_token', 'mytoken')
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({}),
    } as Response)

    await apiGet('/secure')

    const init = spy.mock.calls[0][1]
    expect((init?.headers as Record<string, string>)['Authorization']).toBe('Bearer mytoken')
  })

  test('omits Authorization header when no token is stored', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({}),
    } as Response)

    await apiGet('/public')

    const init = spy.mock.calls[0][1]
    expect((init?.headers as Record<string, string>)['Authorization']).toBeUndefined()
  })
})

// ── apiPost ───────────────────────────────────────────────────────────────────

describe('apiPost', () => {
  test('sends POST with JSON body', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ status: 'ok' }),
    } as Response)

    await apiPost('/action', { key: 'val' })

    const init = spy.mock.calls[0][1]
    expect(init?.method).toBe('POST')
    expect(init?.body).toBe(JSON.stringify({ key: 'val' }))
  })

  test('sends POST with no body when body is omitted', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({}),
    } as Response)

    await apiPost('/ping')

    const init = spy.mock.calls[0][1]
    expect(init?.body).toBeUndefined()
  })
})

// ── apiDelete ─────────────────────────────────────────────────────────────────

describe('apiDelete', () => {
  test('sends DELETE request', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ status: 'deleted' }),
    } as Response)

    await apiDelete('/items/1')

    const init = spy.mock.calls[0][1]
    expect(init?.method).toBe('DELETE')
  })
})
