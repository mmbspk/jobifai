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
    // Clear any pending WS batch so old-account lines cannot flush after the switch.
    this.batchBuf = []
    if (this.batchTimer) {
      clearTimeout(this.batchTimer)
      this.batchTimer = null
    }
    this.notify()
  },

  // wipe() is called by the manual "clear" button. Empties the display without re-seeding.
  // Increments seedNonce to discard any in-flight REST response so it cannot restore lines.
  // Preserves displayedRunId so subsequent WS events for the current run are still accepted
  // and a wrong-run event cannot claim the run slot after the wipe.
  wipe() {
    this.lines = []
    this.batchBuf = []
    if (this.batchTimer) {
      clearTimeout(this.batchTimer)
      this.batchTimer = null
    }
    this.seenIds = new Set()
    this.seedNonce++  // Invalidate any in-flight seed response.
    this.seeded = true // Suppress automatic re-seed; run identity in displayedRunId is preserved.
    this.notify()
  },

  // clear(keepRunId) is called when the Dashboard detects a new run (via status poll).
  // Lines already received for the new run are kept; old-run lines are removed.
  // Also filters the pending WS batch so the next flush doesn't reintroduce old-run lines.
  // Invalidates any in-flight seed response for the old run via seedNonce, then
  // immediately fetches persisted logs for the new run.
  clear(keepRunId: string) {
    const retained = this.lines.filter(l => l.runId === keepRunId)
    const retainedBatch = this.batchBuf.filter(l => l.runId === keepRunId)
    // Rebuild seenIds from all retained content (flushed + pending).
    this.seenIds = new Set(
      [...retained.map(l => l.id), ...retainedBatch.map(l => l.id)].filter(Boolean),
    )
    this.lines = retained
    this.batchBuf = retainedBatch
    if (!this.batchBuf.length && this.batchTimer) {
      clearTimeout(this.batchTimer)
      this.batchTimer = null
    }
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

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    const token = getToken()
    const url = token ? `${wsUrl('/ws/logs')}?token=${encodeURIComponent(token)}` : wsUrl('/ws/logs')
    const ws = new WebSocket(url)
    this.ws = ws

    // Capture at connection time so callbacks self-reject after account switch.
    const capturedGen = this.authGen
    const capturedUserId = this.userId

    ws.onopen = () => {
      if (capturedGen !== this.authGen) return
      this.connected = true
      this.retryDelay = 500
      // Seed on reconnect so a failed or dropped initial seed is retried automatically.
      if (!this.seeded) scheduleSeed(capturedUserId, capturedGen)
      this.notify()
    }

    ws.onmessage = (e) => {
      if (capturedGen !== this.authGen) return
      const parsed = parseLogLine(String(e.data))
      if (!parsed) return
      const { id, runId } = parsed

      // Deduplicate: skip lines already in the buffer (from seed)
      if (id && this.seenIds.has(id)) return

      // Reject lines for a different run; run transitions go through status-poll + clear().
      if (runId && runId !== this.displayedRunId && this.displayedRunId !== '') return

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
      if (capturedGen !== this.authGen) return
      this.connected = false
      this.ws = null
      this.notify()
      const delay = this.retryDelay
      this.retryDelay = Math.min(delay * 2, 10000)
      this.retryTimer = setTimeout(() => this.connect(), delay)
    }

    ws.onerror = () => ws.close()
  },

  // disconnect() closes the WS and cancels the retry timer without reconnecting.
  // Called on logout so the socket is not left open with no authenticated user.
  disconnect() {
    if (this.ws) {
      this.ws.onopen = null
      this.ws.onmessage = null
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
    this.notify()
  },

  closeAndRebind() {
    if (this.ws) {
      // Null all handlers before close so in-flight events cannot mutate state for the new user.
      this.ws.onopen = null
      this.ws.onmessage = null
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

// notifyLogout is called by AuthContext when the user session ends for any reason
// (explicit logout, token expiry, force-logout). Resets the log store and closes
// the WebSocket without reconnecting so the socket is not left open with no user.
// Safe to call from outside the React tree (no hooks, no re-render required).
export function notifyLogout(): void {
  if (store.userId !== '') {
    store.reset('')
    store.disconnect()
  }
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
  // On logout (userId = ''), reset store and disconnect without reconnecting so
  // the socket is not left open. Update prevUserIdRef to '' so a re-login with
  // the same account still triggers a fresh reset.
  useEffect(() => {
    if (!userId) {
      if (store.userId !== '') {
        store.reset('')
        store.disconnect()
      }
      prevUserIdRef.current = ''
      return
    }
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
  const wipe = useCallback(() => store.wipe(), [])

  return { lines, connected, displayedRunId, clear, wipe }
}
