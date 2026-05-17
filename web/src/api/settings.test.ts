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

import { settingsApi } from './settings'

beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('settingsApi.general.get', () => {
  test('GETs /api/settings/general', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ({}),
    } as Response)

    await settingsApi.general.get()

    expect(spy.mock.calls[0][0]).toBe('/api/settings/general')
    expect(spy.mock.calls[0][1]?.method).toBe('GET')
  })
})

describe('settingsApi.general.set', () => {
  test('POSTs /api/settings/general with body', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ({}),
    } as Response)

    await settingsApi.general.set({ halal_job_filter: true } as never)

    expect(spy.mock.calls[0][0]).toBe('/api/settings/general')
    expect(spy.mock.calls[0][1]?.method).toBe('POST')
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ halal_job_filter: true }))
  })
})

describe('settingsApi.secrets.setApiKey', () => {
  test('POSTs to /api/settings/secrets/api-key with key_type and value', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ({}),
    } as Response)

    await settingsApi.secrets.setApiKey('llm_api_key', 'sk-abc123')

    expect(spy.mock.calls[0][0]).toBe('/api/settings/secrets/api-key')
    expect(spy.mock.calls[0][1]?.method).toBe('POST')
    const body = JSON.parse(spy.mock.calls[0][1]?.body as string)
    expect(body).toEqual({ key_type: 'llm_api_key', value: 'sk-abc123' })
  })
})

describe('settingsApi.locations.suggest', () => {
  test('GETs /api/settings/locations/suggest with encoded q param', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ['Sydney, NSW'],
    } as Response)

    await settingsApi.locations.suggest('Syd ney')

    const url = spy.mock.calls[0][0] as string
    expect(url).toContain('/api/settings/locations/suggest')
    expect(url).toContain('q=Syd%20ney')
  })
})

describe('settingsApi.markets.list', () => {
  test('GETs /api/settings/markets', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => [],
    } as Response)

    await settingsApi.markets.list()

    expect(spy.mock.calls[0][0]).toBe('/api/settings/markets')
  })
})
