import { useCallback, useEffect, useRef, useState } from 'react'
import { wsUrl, getToken } from '../api/client'
import { botApi } from '../api/bot'

export type LogLevel = 'debug' | 'info' | 'success' | 'warning' | 'error' | 'critical'

export interface LogLine {
  id: string       // stable log_id from zerolog hook (shared by WS + REST)
  level: LogLevel
  message: string
  time: string
  raw: string
  llmCall: boolean
  runId: string    // automation_run_id from the log line
  historical: boolean // true for lines seeded from REST (pre-refresh)
}

const MAX_LINES = 500
const BATCH_MS = 80

export function parseLogLine(raw: string): Omit<LogLine, 'historical'> | null {
  try {
    const obj = JSON.parse(raw)
    const level = ((obj.level ?? obj.l ?? 'info') as string).toLowerCase() as LogLevel
    const time = obj.time ?? obj.t ?? new Date().toISOString()
    const base: string = obj.message ?? obj.msg ?? obj.m ?? raw
    const detail: string = obj.error ?? obj.err ?? ''
    const message = detail ? `${base}: ${detail}` : base
    const llmCall: boolean = obj.llm_call === true
    const id: string = obj.log_id ?? ''
    const runId: string = obj.automation_run_id ?? ''
    return { id, level, message, time, raw, llmCall, runId }
  } catch {
    return null
  }
}

// ── Module-level singleton ────────────────────────────────────────────────────

type Listener = (lines: LogLine[], displayedRunId: string, connected: boolean) => void

const store = {
  userId: '',
  authGen: 0,            // incremented on reset (account switch)
  displayedRunId: '',    // run ID for lines currently in buffer
  seeded: false,         // guards against duplicate seed requests
  seedNonce: 0,          // incremented on reset/clear; rejects stale seed responses
  seenIds: new Set<string>(),  // IDs from all accepted lines (WS + seed)

  lines: [] as LogLine[],
  connected: false,
  listeners: new Set<Listener>(),
  ws: null as WebSocket | null,
  retryDelay: 500,
  retryTimer: null as ReturnType<typeof setTimeout> | null,
  batchBuf: [] as LogLine[],
  batchTimer: null as ReturnType<typeof setTimeout> | null,

  notify() {
    for (const fn of this.listeners) fn(this.lines, this.displayedRunId, this.connected)
  },

  flush() {
    if (!this.batchBuf.length) return
    const incoming = this.batchBuf.splice(0)
    const next = [...this.lines, ...incoming]
    if (next.length > MAX_LINES) {
      const pruned = next.slice(next.length - MAX_LINES)
      // Remove seenIds for pruned lines to keep the set bounded
      const keptIds = new Set(pruned.map(l => l.id).filter(Boolean))
      for (const id of this.seenIds) {
        if (!keptIds.has(id)) this.seenIds.delete(id)
      }
      this.lines = pruned
    } else {
      this.lines = next
    }
    this.notify()
  },

  // reset() is called on account switch. Invalidates all pending seeds and WS callbacks.
  reset(userId: string) {
    this.userId = userId
    this.authGen++
    this.displayedRunId = ''
    this.seeded = false
    this.seedNonce++
    this.seenIds = new Set()
    this.lines = []
    this.notify()
  },

  // clear(keepRunId) is called when the Dashboard detects a new run (via status poll).
  // Lines already received for the new run are kept; old-run lines are removed.
  // Invalidates any in-flight seed response for the old run via seedNonce, then
  // immediately fetches persisted logs for the new run.
  clear(keepRunId: string) {
    const retained = this.lines.filter(l => l.runId === keepRunId)
    this.seenIds = new Set(retained.map(l => l.id).filter(Boolean))
    this.lines = retained
    this.displayedRunId = keepRunId
    this.seeded = false
    this.seedNonce++
    this.notify()
    // Fetch persisted lines for the new run (nonce already incremented above).
    scheduleSeed(this.userId, this.authGen)
  },

  // seed() applies a REST response. Validates userId, authGen, seedNonce, and runId.
  // Stale responses (account switch, clear() already called for a newer run) are silently dropped.
  seed(userId: string, gen: number, runId: string, lines: LogLine[], nonce: number) {
    if (userId !== this.userId || gen !== this.authGen || nonce !== this.seedNonce) return
    // If we've already adopted a different run via WS, reject seed for the old one.
    if (this.displayedRunId !== '' && this.displayedRunId !== runId) return

    const deduped = lines.filter(l => !this.seenIds.has(l.id))
    if (deduped.length === 0) {
      // Nothing new — still establish displayedRunId if not set
      if (this.displayedRunId === '' && runId !== '') this.displayedRunId = runId
      this.notify()
      return
    }
    for (const l of deduped) {
      if (l.id) this.seenIds.add(l.id)
    }
    // Prepend historical lines (oldest first), cap at MAX_LINES
    const combined = [...deduped, ...this.lines]
    if (combined.length > MAX_LINES) {
      const pruned = combined.slice(combined.length - MAX_LINES)
      const keptIds = new Set(pruned.map(l => l.id).filter(Boolean))
      for (const id of this.seenIds) {
        if (!keptIds.has(id)) this.seenIds.delete(id)
      }
      this.lines = pruned
    } else {
      this.lines = combined
    }
    if (this.displayedRunId === '') this.displayedRunId = runId
    this.notify()
  },

  // adoptRun() is called when WS delivers a line for a run different from displayedRunId.
  // Clears the buffer (keeping lines for the new run), invalidates pending seeds, and
  // triggers a fresh seed for the new run — recovering any lines rejected before the switch.
  adoptRun(newRunId: string, userId: string, gen: number) {
    const retained = this.lines.filter(l => l.runId === newRunId)
    this.seenIds = new Set(retained.map(l => l.id).filter(Boolean))
    this.lines = retained
    this.displayedRunId = newRunId
    this.seeded = false
    this.seedNonce++
    this.notify()
    // Trigger a fresh seed to recover persisted lines for newRunId that arrived
    // before this WS event (i.e., were rejected while displayedRunId was the old run).
    scheduleSeed(userId, gen)
  },

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    const token = getToken()
    const url = token ? `${wsUrl('/ws/logs')}?token=${encodeURIComponent(token)}` : wsUrl('/ws/logs')
    const ws = new WebSocket(url)
    this.ws = ws

    ws.onopen = () => {
      this.connected = true
      this.retryDelay = 500
      this.notify()
    }

    ws.onmessage = (e) => {
      const parsed = parseLogLine(String(e.data))
      if (!parsed) return
      const { id, runId } = parsed

      // Deduplicate: skip lines already in the buffer (from seed)
      if (id && this.seenIds.has(id)) return

      if (runId && runId !== this.displayedRunId && this.displayedRunId !== '') {
        // New run detected through WS before status poll — adopt it.
        // The line will be re-added after adoptRun clears and rebuilds the buffer.
        const capturedUserId = this.userId
        const capturedGen = this.authGen
        this.adoptRun(runId, capturedUserId, capturedGen)
        // Re-process this line under the new run context.
      }

      if (runId && this.displayedRunId === '') {
        this.displayedRunId = runId
      }

      const line: LogLine = { ...parsed, historical: false }
      if (id) this.seenIds.add(id)
      this.batchBuf.push(line)
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

  closeAndRebind() {
    if (this.ws) {
      this.ws.onclose = null
      this.ws.onerror = null
      this.ws.close()
      this.ws = null
    }
    if (this.retryTimer) {
      clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
    this.connected = false
    this.connect()
  },
}

// scheduleSeed dispatches a seed request outside of the store's own methods to
// avoid capturing stale closure variables.
function scheduleSeed(userId: string, gen: number) {
  const nonce = store.seedNonce
  store.seeded = true
  botApi.runLogs().then(data => {
    if (!data?.lines) return
    const lines: LogLine[] = data.lines.map(l => {
      const parsed = parseLogLine(l.raw_json)
      if (!parsed) return null
      return { ...parsed, historical: true }
    }).filter((l): l is LogLine => l !== null)
    store.seed(userId, gen, data.run_id, lines, nonce)
  }).catch(() => {
    // Allow retry on next reconnect or mount
    store.seeded = false
  })
}

// Start the singleton connection immediately when this module is first imported.
store.connect()

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useLogs(userId: string) {
  const [lines, setLines] = useState<LogLine[]>(store.lines)
  const [displayedRunId, setDisplayedRunId] = useState(store.displayedRunId)
  const [connected, setConnected] = useState(store.connected)
  const mountedRef = useRef(true)
  const prevUserIdRef = useRef(userId)

  // Handle account switch: reset store and rebind WS for the new user.
  useEffect(() => {
    if (!userId) return
    if (userId !== prevUserIdRef.current || userId !== store.userId) {
      prevUserIdRef.current = userId
      store.reset(userId)
      store.closeAndRebind()
    }
  }, [userId])

  // Subscribe to store notifications
  useEffect(() => {
    mountedRef.current = true
    const listener: Listener = (l, rid, conn) => {
      if (!mountedRef.current) return
      setLines([...l])
      setDisplayedRunId(rid)
      setConnected(conn)
    }
    store.listeners.add(listener)
    setLines([...store.lines])
    setDisplayedRunId(store.displayedRunId)
    setConnected(store.connected)
    return () => {
      mountedRef.current = false
      store.listeners.delete(listener)
    }
  }, [])

  // Seed on mount (or after reset) if not already seeded.
  useEffect(() => {
    if (!userId || store.seeded) return
    scheduleSeed(userId, store.authGen)
  }, [userId])

  const clear = useCallback((keepRunId: string) => store.clear(keepRunId), [])

  return { lines, connected, displayedRunId, clear }
}
