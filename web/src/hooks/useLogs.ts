import { useCallback, useEffect, useRef, useState } from 'react'
import { wsUrl } from '../api/client'

export type LogLevel = 'debug' | 'info' | 'success' | 'warning' | 'error' | 'critical'

export interface LogLine {
  level: LogLevel
  message: string
  time: string
  raw: string
}

const MAX_LINES = 500
const BATCH_MS = 80

function parseLogLine(raw: string): LogLine {
  try {
    const obj = JSON.parse(raw)
    const level = ((obj.level ?? obj.l ?? 'info') as string).toLowerCase() as LogLevel
    const time = obj.time ?? obj.t ?? new Date().toISOString()
    const base: string = obj.message ?? obj.msg ?? obj.m ?? raw
    const detail: string = obj.error ?? obj.err ?? ''
    const message = detail ? `${base}: ${detail}` : base
    return { level, message, time, raw }
  } catch {
    return { level: 'info', message: raw, time: new Date().toISOString(), raw }
  }
}

// ── Module-level singleton ────────────────────────────────────────────────────
// Keeps the WebSocket and log buffer alive across component mount/unmount cycles
// so navigation never loses accumulated logs.

type Listener = (lines: LogLine[]) => void

const store = {
  lines: [] as LogLine[],
  connected: false,
  listeners: new Set<Listener>(),
  ws: null as WebSocket | null,
  retryDelay: 500,
  retryTimer: null as ReturnType<typeof setTimeout> | null,
  batchBuf: [] as LogLine[],
  batchTimer: null as ReturnType<typeof setTimeout> | null,

  notify() {
    for (const fn of this.listeners) fn(this.lines)
  },

  flush() {
    if (!this.batchBuf.length) return
    const incoming = this.batchBuf.splice(0)
    const next = [...this.lines, ...incoming]
    this.lines = next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
    this.notify()
  },

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    const ws = new WebSocket(wsUrl('/ws/logs'))
    this.ws = ws

    ws.onopen = () => {
      this.connected = true
      this.retryDelay = 500
      this.notify()
    }

    ws.onmessage = (e) => {
      this.batchBuf.push(parseLogLine(String(e.data)))
      if (!this.batchTimer) {
        this.batchTimer = setTimeout(() => {
          this.batchTimer = null
          this.flush()
        }, BATCH_MS)
      }
    }

    ws.onclose = () => {
      this.connected = false
      this.ws = null
      this.notify()
      const delay = this.retryDelay
      this.retryDelay = Math.min(delay * 2, 10000)
      this.retryTimer = setTimeout(() => this.connect(), delay)
    }

    ws.onerror = () => ws.close()
  },

  clear() {
    this.lines = []
    this.notify()
  },
}

// Start the singleton connection immediately when this module is first imported.
store.connect()

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useLogs() {
  const [lines, setLines] = useState<LogLine[]>(store.lines)
  const [connected, setConnected] = useState(store.connected)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    const listener: Listener = (l) => {
      if (!mountedRef.current) return
      setLines([...l])
      setConnected(store.connected)
    }
    store.listeners.add(listener)
    // Sync immediately in case store updated between renders
    setLines([...store.lines])
    setConnected(store.connected)
    return () => {
      mountedRef.current = false
      store.listeners.delete(listener)
    }
  }, [])

  const clear = useCallback(() => store.clear(), [])

  return { lines, connected, clear }
}
