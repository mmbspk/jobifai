import { vi, describe, test, expect, beforeEach } from 'vitest'

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

import { usageApi } from './usage'

beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('usageApi.session', () => {
  test('GETs /api/usage/session', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ input_tokens: 100, output_tokens: 50, calls: 2 }),
    } as Response)

    await usageApi.session()

    expect(spy.mock.calls[0][0]).toBe('/api/usage/session')
    expect(spy.mock.calls[0][1]?.method).toBe('GET')
  })
})

describe('usageApi.totals', () => {
  test('GETs /api/usage/totals', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ input_tokens: 500, output_tokens: 200, calls: 10 }),
    } as Response)

    await usageApi.totals()

    expect(spy.mock.calls[0][0]).toBe('/api/usage/totals')
    expect(spy.mock.calls[0][1]?.method).toBe('GET')
  })
})
