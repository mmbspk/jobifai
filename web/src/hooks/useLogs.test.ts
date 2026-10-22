import { renderHook, act, waitFor } from '@testing-library/react'
import { vi, describe, test, expect, beforeEach } from 'vitest'
import type { Mock } from 'vitest'

// ---------- MockWS setup (must be hoisted so it runs before module imports) ----------

const wsContainer = vi.hoisted(() => {
  const container: {
    current: {
      readyState: number
      onopen: (() => void) | null
      onmessage: ((e: { data: string }) => void) | null
      onclose: (() => void) | null
      onerror: (() => void) | null
      close(): void
    } | null
  } = { current: null }

  function MockWS(this: {
    readyState: number
    onopen: (() => void) | null
    onmessage: ((e: { data: string }) => void) | null
    onclose: (() => void) | null
    onerror: (() => void) | null
    close(): void
  }) {
    this.readyState = 1
    this.onopen = null
    this.onmessage = null
    this.onclose = null
    this.onerror = null
    this.close = () => {}
    container.current = this
  }
  ;(MockWS as unknown as { OPEN: number; CONNECTING: number }).OPEN = 1
  ;(MockWS as unknown as { OPEN: number; CONNECTING: number }).CONNECTING = 0
  ;(globalThis as Record<string, unknown>).WebSocket = MockWS
  return container
})

vi.mock('../api/client', () => ({
  getToken: () => null,
  wsUrl: (path: string) => `ws://localhost${path}`,
}))

vi.mock('../api/bot', () => ({
  botApi: {
    runLogs: vi.fn(),
  },
}))

import { useLogs, parseLogLine } from './useLogs'
import { botApi } from '../api/bot'

// ---------- Helpers ----------

let userCounter = 0
function nextId() { return `u${++userCounter}` }

function wsLine(opts: { logId: string; runId: string; level?: string }) {
  return JSON.stringify({
    level: opts.level ?? 'info',
    message: 'seek: processing job',
    log_id: opts.logId,
    automation_run_id: opts.runId,
    user_id: 'irrelevant',
    time: new Date().toISOString(),
  })
}

function makeSeedResponse(runId: string, logIds: string[]) {
  return {
    run_id: runId,
    lines: logIds.map(id => ({
      id,
      level: 'info',
      message: 'seed line',
      raw_json: wsLine({ logId: id, runId }),
      created_at: new Date().toISOString(),
    })),
  }
}

function deliver(data: string) {
  wsContainer.current?.onmessage?.({ data })
}

// ---------- parseLogLine unit tests ----------

describe('parseLogLine', () => {
  test('parses level and message', () => {
    const line = parseLogLine(JSON.stringify({ level: 'info', message: 'job started' }))
    expect(line?.level).toBe('info')
    expect(line?.message).toBe('job started')
    expect(line?.llmCall).toBe(false)
  })

  test('accepts abbreviated field names (l, msg, t)', () => {
    const line = parseLogLine(JSON.stringify({ l: 'warn', msg: 'slow', t: '2026-05-07T10:00:00Z' }))
    expect(line?.level).toBe('warn')
    expect(line?.message).toBe('slow')
    expect(line?.time).toBe('2026-05-07T10:00:00Z')
  })

  test('appends error detail to message', () => {
    const line = parseLogLine(JSON.stringify({ level: 'error', message: 'db query failed', error: 'connection refused' }))
    expect(line?.message).toBe('db query failed: connection refused')
  })

  test('defaults level to info', () => {
    const line = parseLogLine(JSON.stringify({ message: 'hello' }))
    expect(line?.level).toBe('info')
  })

  test('normalises level to lowercase', () => {
    const line = parseLogLine(JSON.stringify({ level: 'ERROR', message: 'oops' }))
    expect(line?.level).toBe('error')
  })

  test('sets llmCall true', () => {
    const line = parseLogLine(JSON.stringify({ level: 'info', message: 'scoring', llm_call: true }))
    expect(line?.llmCall).toBe(true)
  })

  test('id is the log_id string from the JSON', () => {
    const line = parseLogLine(JSON.stringify({ message: 'a', log_id: 'abc-123' }))
    expect(line?.id).toBe('abc-123')
  })

  test('id is empty string when log_id absent', () => {
    const line = parseLogLine(JSON.stringify({ message: 'b' }))
    expect(line?.id).toBe('')
  })

  test('runId is automation_run_id string', () => {
    const line = parseLogLine(JSON.stringify({ message: 'x', automation_run_id: 'run-99' }))
    expect(line?.runId).toBe('run-99')
  })

  test('returns null for non-JSON input', () => {
    expect(parseLogLine('not json')).toBeNull()
  })

  test('preserves raw input', () => {
    const raw = JSON.stringify({ level: 'info', message: 'raw preserved' })
    expect(parseLogLine(raw)?.raw).toBe(raw)
  })
})

// ---------- Store behaviour tests ----------

describe('useLogs store', () => {
  beforeEach(() => {
    // resetAllMocks clears implementations AND once-queues (clearAllMocks only clears call history).
    vi.resetAllMocks()
    ;(botApi.runLogs as Mock).mockResolvedValue({ run_id: '', lines: [] })
  })

  test('WS dedup: seed and WS carry same log_id — only one line in buffer', async () => {
    const uid = nextId()
    const seed = makeSeedResponse('run-A', ['la1'])
    ;(botApi.runLogs as Mock).mockResolvedValue(seed)

    const { result } = renderHook(() => useLogs(uid))

    await waitFor(() => expect(result.current.lines).toHaveLength(1))
    expect(result.current.lines[0].id).toBe('la1')

    // WS delivers same line — should be deduped
    deliver(wsLine({ logId: 'la1', runId: 'run-A' }))
    // Flush the batch timer (80 ms) and assert no duplicate.
    await waitFor(() => expect(result.current.lines).toHaveLength(1))
  })

  test('stale run-A seed rejected after run-B WS line is received', async () => {
    const uid = nextId()
    let resolveA!: (v: unknown) => void
    ;(botApi.runLogs as Mock)
      .mockImplementationOnce(() => new Promise(r => { resolveA = r })) // initial seed — blocked
      .mockResolvedValue({ run_id: 'run-B', lines: [] })

    const { result } = renderHook(() => useLogs(uid))

    // WS delivers run-B line. displayedRunId was '' so adoptRun is NOT called;
    // notify() only fires after the 80 ms batch timer — use waitFor.
    deliver(wsLine({ logId: 'lb1', runId: 'run-B' }))
    await waitFor(() => expect(result.current.displayedRunId).toBe('run-B'))

    // Resolve the blocked run-A seed. displayedRunId is now 'run-B' → seed rejected.
    await act(async () => { resolveA(makeSeedResponse('run-A', ['la1', 'la2'])) })

    await waitFor(() => expect(result.current.lines.map(l => l.id)).toContain('lb1'))
    const ids = result.current.lines.map(l => l.id)
    expect(ids).not.toContain('la1')
    expect(ids).not.toContain('la2')
  })

  test('WS event for different run triggers adoptRun — lines switch to the new run', async () => {
    // When displayedRunId IS set and WS delivers a line for a different run, the store
    // calls adoptRun() (immediate notify), clears old lines, and re-seeds for the new run.
    const uid = nextId()
    const seedA = makeSeedResponse('run-A', ['la1'])
    ;(botApi.runLogs as Mock)
      .mockResolvedValueOnce(seedA)                          // initial seed
      .mockResolvedValue({ run_id: 'run-B', lines: [] })     // seed after adoptRun

    const { result } = renderHook(() => useLogs(uid))
    await waitFor(() => expect(result.current.lines).toHaveLength(1))
    expect(result.current.displayedRunId).toBe('run-A')

    // Deliver run-B event while run-A is displayed.
    // adoptRun calls notify() synchronously → displayedRunId updates without batch delay.
    deliver(wsLine({ logId: 'lb1', runId: 'run-B' }))
    await waitFor(() => expect(result.current.displayedRunId).toBe('run-B'))

    // run-A lines cleared; lb1 appears after the batch flush.
    await waitFor(() => expect(result.current.lines.map(l => l.id)).toContain('lb1'))
    expect(result.current.lines.map(l => l.id)).not.toContain('la1')
  })

  test('clear() triggers a new seed fetch for the new run', async () => {
    const uid = nextId()
    const seedA = makeSeedResponse('run-A', ['la1'])
    const seedB = makeSeedResponse('run-B', ['lb-persisted'])
    ;(botApi.runLogs as Mock)
      .mockResolvedValueOnce(seedA)
      .mockResolvedValueOnce(seedB)

    const { result } = renderHook(() => useLogs(uid))
    await waitFor(() => expect(result.current.lines).toHaveLength(1))

    await act(async () => { result.current.clear('run-B') })

    await waitFor(() => expect(botApi.runLogs).toHaveBeenCalledTimes(2))
    // waitFor callback must throw (via expect) so it retries until the seed is applied.
    await waitFor(() => expect(result.current.lines.map(l => l.id)).toContain('lb-persisted'))
    expect(result.current.lines.map(l => l.id)).not.toContain('la1')
  })

  test('early run-B WS lines are recovered after adoptRun seeds persisted logs', async () => {
    const uid = nextId()
    const seedA = makeSeedResponse('run-A', ['la1', 'la2'])
    const seedB = makeSeedResponse('run-B', ['lb-persist'])
    ;(botApi.runLogs as Mock)
      .mockResolvedValueOnce(seedA)
      .mockResolvedValueOnce(seedB)

    const { result } = renderHook(() => useLogs(uid))
    await waitFor(() => expect(result.current.lines).toHaveLength(2))

    // adoptRun fires immediately on the WS event (displayedRunId was 'run-A', not '').
    deliver(wsLine({ logId: 'lb-ws', runId: 'run-B' }))

    await waitFor(() => expect(botApi.runLogs).toHaveBeenCalledTimes(2))
    await waitFor(() => {
      const ids = result.current.lines.map(l => l.id)
      expect(ids).toContain('lb-persist')
      expect(ids).toContain('lb-ws')
    })
    const ids = result.current.lines.map(l => l.id)
    expect(ids).not.toContain('la1')
    expect(ids).not.toContain('la2')
  })

  test('account switch: in-flight seed for old user is rejected', async () => {
    const uid1 = nextId()
    const uid2 = nextId()

    let resolveOld!: (v: unknown) => void
    ;(botApi.runLogs as Mock)
      .mockImplementationOnce(() => new Promise(r => { resolveOld = r }))
      .mockResolvedValue({ run_id: '', lines: [] })

    const { result, rerender } = renderHook(({ id }: { id: string }) => useLogs(id), {
      initialProps: { id: uid1 },
    })

    // Wait until the first (blocked) seed call is made so resolveOld is assigned.
    await waitFor(() => expect(botApi.runLogs).toHaveBeenCalledTimes(1))

    await act(async () => { rerender({ id: uid2 }) })

    await act(async () => { resolveOld(makeSeedResponse('run-A', ['la1'])) })

    expect(result.current.lines.map(l => l.id)).not.toContain('la1')
  })

  test('failed seed resets seeded flag so next mount retries', async () => {
    const uid = nextId()
    ;(botApi.runLogs as Mock)
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValue(makeSeedResponse('run-A', ['la1']))

    const { unmount } = renderHook(() => useLogs(uid))

    // Wait for the first call (rejection); then flush the rejection microtask.
    await waitFor(() => expect(botApi.runLogs).toHaveBeenCalledTimes(1))
    await act(async () => { await Promise.resolve() })

    // Remount — seeded=false (catch handler ran) → triggers retry.
    unmount()
    const { result: result2 } = renderHook(() => useLogs(uid))

    await waitFor(() => expect(result2.current.lines).toHaveLength(1))
    expect(result2.current.lines[0].id).toBe('la1')
  })

  test('displayedRunId reflects seeded run, not just active status run', async () => {
    const uid = nextId()
    ;(botApi.runLogs as Mock).mockResolvedValue(makeSeedResponse('run-prev', ['lp1']))

    const { result } = renderHook(() => useLogs(uid))
    await waitFor(() => expect(result.current.displayedRunId).toBe('run-prev'))
  })

  test('zero-line history: seed returns run_id with empty lines — displayedRunId set', async () => {
    const uid = nextId()
    ;(botApi.runLogs as Mock).mockResolvedValue({ run_id: 'run-empty', lines: [] })

    const { result } = renderHook(() => useLogs(uid))
    await waitFor(() => expect(botApi.runLogs).toHaveBeenCalledTimes(1))
    // displayedRunId should be set even with 0 lines
    await waitFor(() => expect(result.current.displayedRunId).toBe('run-empty'))
    expect(result.current.lines).toHaveLength(0)
  })
})
