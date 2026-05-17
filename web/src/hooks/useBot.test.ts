import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, test, expect, beforeEach, describe } from 'vitest'
import React from 'react'

vi.mock('../api/bot', () => ({
  botApi: {
    status: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
  },
}))

import { useBot } from './useBot'
import { botApi } from '../api/bot'

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children)
}

beforeEach(() => {
  vi.clearAllMocks()
})

test('returns undefined status initially', () => {
  vi.mocked(botApi.status).mockResolvedValue({ state: 'idle', daily_limit: 40 } as never)
  const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
  expect(result.current.status).toBeUndefined()
})

test('populates status from the query', async () => {
  vi.mocked(botApi.status).mockResolvedValue({ state: 'idle', daily_limit: 40 } as never)
  const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
  await waitFor(() => expect(result.current.status).toBeDefined())
  expect(result.current.status?.state).toBe('idle')
})

describe('start mutation', () => {
  test('calls botApi.start with the given platform', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'idle', daily_limit: 40 } as never)
    vi.mocked(botApi.start).mockResolvedValue(undefined as never)
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    await act(() => result.current.start.mutateAsync('linkedin'))
    expect(botApi.start).toHaveBeenCalledWith('linkedin')
  })

  test('clears startError on success', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'idle', daily_limit: 40 } as never)
    vi.mocked(botApi.start).mockResolvedValue(undefined as never)
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    await act(() => result.current.start.mutateAsync('seek'))
    expect(result.current.startError).toBeNull()
  })

  test('sets startError when start fails', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'idle', daily_limit: 40 } as never)
    vi.mocked(botApi.start).mockRejectedValue(new Error('bot is already running'))
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    try { await act(() => result.current.start.mutateAsync('linkedin')) } catch { /* expected */ }
    await waitFor(() => expect(result.current.startError).toBe('bot is already running'))
  })
})

describe('stop mutation', () => {
  test('calls botApi.stop', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'running', daily_limit: 40 } as never)
    vi.mocked(botApi.stop).mockResolvedValue(undefined as never)
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    await act(() => result.current.stop.mutateAsync())
    expect(botApi.stop).toHaveBeenCalledTimes(1)
  })
})

describe('pause / resume mutations', () => {
  test('calls botApi.pause', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'running', daily_limit: 40 } as never)
    vi.mocked(botApi.pause).mockResolvedValue(undefined as never)
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    await act(() => result.current.pause.mutateAsync())
    expect(botApi.pause).toHaveBeenCalledTimes(1)
  })

  test('calls botApi.resume', async () => {
    vi.mocked(botApi.status).mockResolvedValue({ state: 'paused', daily_limit: 40 } as never)
    vi.mocked(botApi.resume).mockResolvedValue(undefined as never)
    const { result } = renderHook(() => useBot(), { wrapper: makeWrapper() })
    await act(() => result.current.resume.mutateAsync())
    expect(botApi.resume).toHaveBeenCalledTimes(1)
  })
})
