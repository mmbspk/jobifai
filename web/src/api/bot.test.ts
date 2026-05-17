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

import { botApi } from './bot'

function mockOk(data: unknown) {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
    ok: true, status: 200,
    json: async () => data,
  } as Response)
}

beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('botApi.status', () => {
  test('GETs /api/bot/status', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ state: 'idle' }),
    } as Response)

    await botApi.status()

    expect(spy.mock.calls[0][0]).toBe('/api/bot/status')
    const init = spy.mock.calls[0][1]
    expect(init?.method).toBe('GET')
  })
})

describe('botApi.start', () => {
  test('POSTs to /api/bot/start with platform', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({}),
    } as Response)

    await botApi.start('linkedin')

    expect(spy.mock.calls[0][0]).toBe('/api/bot/start')
    const init = spy.mock.calls[0][1]
    expect(init?.method).toBe('POST')
    expect(init?.body).toBe(JSON.stringify({ platform: 'linkedin' }))
  })
})

describe('botApi.stop', () => {
  test('POSTs to /api/bot/stop', async () => {
    mockOk({})
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ({}),
    } as Response)

    await botApi.stop()

    expect(spy.mock.calls[0][0]).toBe('/api/bot/stop')
    expect(spy.mock.calls[0][1]?.method).toBe('POST')
  })
})

describe('botApi.applyFromURL', () => {
  test('POSTs with url, market, and force', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ status: 'applied' }),
    } as Response)

    await botApi.applyFromURL('https://example.com/job/1', 'generic', true)

    expect(spy.mock.calls[0][0]).toBe('/api/bot/apply-url')
    expect(spy.mock.calls[0][1]?.method).toBe('POST')
    expect(spy.mock.calls[0][1]?.body).toBe(
      JSON.stringify({ url: 'https://example.com/job/1', market: 'generic', force: true })
    )
  })

  test('force defaults to false', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ status: 'applied' }),
    } as Response)

    await botApi.applyFromURL('https://example.com/job/2', 'generic')

    const body = JSON.parse(spy.mock.calls[0][1]?.body as string)
    expect(body.force).toBe(false)
  })
})
