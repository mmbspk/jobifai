import { vi, describe, test, expect } from 'vitest'

// Hoist setup runs before any module is evaluated, so WebSocket and
// getToken are in place before useLogs.ts module-level code fires.
vi.hoisted(() => {
  class MockWS {
    static readonly OPEN = 1
    static readonly CONNECTING = 0
    readyState = 1
    onopen: (() => void) | null = null
    onmessage: ((e: { data: string }) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    close(): void { /* no-op in tests */ }
  }
  ;(globalThis as Record<string, unknown>).WebSocket = MockWS
})

vi.mock('../api/client', () => ({
  getToken: () => null,
  wsUrl: (path: string) => `ws://localhost${path}`,
}))

import { parseLogLine } from './useLogs'

describe('parseLogLine — JSON input', () => {
  test('parses level and message', () => {
    const line = parseLogLine(JSON.stringify({ level: 'info', message: 'job started' }))
    expect(line.level).toBe('info')
    expect(line.message).toBe('job started')
    expect(line.llmCall).toBe(false)
  })

  test('accepts abbreviated field names (l, msg, t)', () => {
    const line = parseLogLine(JSON.stringify({ l: 'warn', msg: 'slow response', t: '2026-05-07T10:00:00Z' }))
    expect(line.level).toBe('warn')
    expect(line.message).toBe('slow response')
    expect(line.time).toBe('2026-05-07T10:00:00Z')
  })

  test('accepts m as message alias', () => {
    const line = parseLogLine(JSON.stringify({ level: 'debug', m: 'trace' }))
    expect(line.message).toBe('trace')
  })

  test('appends error detail to message when error field present', () => {
    const line = parseLogLine(JSON.stringify({ level: 'error', message: 'db query failed', error: 'connection refused' }))
    expect(line.message).toBe('db query failed: connection refused')
  })

  test('appends err detail to message when err field present', () => {
    const line = parseLogLine(JSON.stringify({ level: 'error', message: 'timeout', err: 'deadline exceeded' }))
    expect(line.message).toBe('timeout: deadline exceeded')
  })

  test('defaults level to info when absent', () => {
    const line = parseLogLine(JSON.stringify({ message: 'hello' }))
    expect(line.level).toBe('info')
  })

  test('normalises level to lowercase', () => {
    const line = parseLogLine(JSON.stringify({ level: 'ERROR', message: 'oops' }))
    expect(line.level).toBe('error')
  })

  test('sets llmCall true when llm_call is true', () => {
    const line = parseLogLine(JSON.stringify({ level: 'info', message: 'scoring', llm_call: true }))
    expect(line.llmCall).toBe(true)
  })

  test('sets llmCall false when llm_call is absent', () => {
    const line = parseLogLine(JSON.stringify({ level: 'info', message: 'no llm' }))
    expect(line.llmCall).toBe(false)
  })

  test('preserves raw input', () => {
    const raw = JSON.stringify({ level: 'info', message: 'raw preserved' })
    const line = parseLogLine(raw)
    expect(line.raw).toBe(raw)
  })

  test('assigns incrementing ids', () => {
    const a = parseLogLine(JSON.stringify({ message: 'a' }))
    const b = parseLogLine(JSON.stringify({ message: 'b' }))
    expect(b.id).toBeGreaterThan(a.id)
  })
})

describe('parseLogLine — non-JSON fallback', () => {
  test('uses raw string as message', () => {
    const line = parseLogLine('plain text log line')
    expect(line.message).toBe('plain text log line')
    expect(line.level).toBe('info')
    expect(line.llmCall).toBe(false)
  })

  test('empty string becomes empty message', () => {
    const line = parseLogLine('')
    expect(line.message).toBe('')
  })
})
