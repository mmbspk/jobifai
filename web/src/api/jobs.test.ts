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

import { jobsApi } from './jobs'

beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('jobsApi.applied', () => {
  test('GETs /api/jobs/applied', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => [],
    } as Response)

    await jobsApi.applied()

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/applied')
    expect(spy.mock.calls[0][1]?.method).toBe('GET')
  })

  test('appends platform query param when provided', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => [],
    } as Response)

    await jobsApi.applied({ platform: 'seek' })

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/applied?platform=seek')
  })
})

describe('jobsApi.deleteApplied', () => {
  test('DELETEs /api/jobs/applied/:id', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => ({ status: 'deleted' }),
    } as Response)

    await jobsApi.deleteApplied('job-123')

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/applied/job-123')
    expect(spy.mock.calls[0][1]?.method).toBe('DELETE')
  })
})

describe('jobsApi.skipped', () => {
  test('GETs /api/jobs/skipped', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200, json: async () => [],
    } as Response)

    await jobsApi.skipped()

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/skipped')
  })
})

describe('jobsApi.stats', () => {
  test('GETs /api/jobs/stats', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ total_applied: 5, applied_today: 1, total_skipped: 2 }),
    } as Response)

    await jobsApi.stats()

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/stats')
    expect(spy.mock.calls[0][1]?.method).toBe('GET')
  })
})

describe('jobsApi.get', () => {
  test('GETs /api/jobs/:id', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true, status: 200,
      json: async () => ({ id: 'abc', status: 'applied' }),
    } as Response)

    await jobsApi.get('abc')

    expect(spy.mock.calls[0][0]).toBe('/api/jobs/abc')
  })
})
