import { useEffect, useRef, useState } from 'react'
import { cn } from '../lib'
import type { LogLine, LogLevel } from '../hooks/useLogs'

const LEVEL_STYLE: Record<LogLevel, string> = {
  debug:    'text-[var(--color-text-dim)]',
  info:     'text-sky-400',
  success:  'text-emerald-400',
  warning:  'text-amber-400',
  error:    'text-red-400',
  critical: 'text-red-300 font-bold',
}

const LEVEL_PREFIX: Record<LogLevel, string> = {
  debug:    'DBG',
  info:     'INF',
  success:  'OK ',
  warning:  'WRN',
  error:    'ERR',
  critical: 'CRT',
}

function isSuccessMessage(msg: string): boolean {
  return msg.includes('submitted successfully') ||
         msg.includes('submitted ✓') ||
         msg.includes('applied ✓')
}

function getBadgeClass(line: LogLine): string {
  if (line.llmCall) return line.message.includes('call failed') ? 'text-red-400' : 'text-violet-400'
  if (isSuccessMessage(line.message)) return 'text-emerald-400'
  return LEVEL_STYLE[line.level]
}

function getMessageClass(line: LogLine): string {
  if (line.llmCall) return line.message.includes('call failed') ? 'text-red-400' : 'text-violet-400'
  if (isSuccessMessage(line.message)) return 'text-emerald-400'
  if (line.level === 'error' || line.level === 'critical') return LEVEL_STYLE[line.level]
  return 'text-[var(--color-text-muted)]'
}

function getBadgeLabel(line: LogLine): string {
  if (line.llmCall) return 'LLM'
  if (isSuccessMessage(line.message)) return 'OK '
  return LEVEL_PREFIX[line.level]
}

function escapeHtml(s: string): string {
  return s.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}

function buildHighlightedHtml(text: string, query: string): string {
  if (!query) return escapeHtml(text)
  const lower = text.toLowerCase()
  const lowerQ = query.toLowerCase()
  let result = ''
  let i = 0
  while (i < text.length) {
    const idx = lower.indexOf(lowerQ, i)
    if (idx === -1) { result += escapeHtml(text.slice(i)); break }
    result += escapeHtml(text.slice(i, idx))
    result += `<mark class="bg-amber-400/30 text-amber-200 rounded-sm">${escapeHtml(text.slice(idx, idx + query.length))}</mark>`
    i = idx + query.length
  }
  return result
}

interface Props {
  readonly lines: LogLine[]
  readonly connected: boolean
  readonly onClear: () => void
  readonly className?: string
  readonly currentJob?: { company: string; role: string } | null
}

export function LogPanel({ lines, connected, onClear, className, currentJob }: Props) {
  const [autoScroll, setAutoScroll] = useState(true)
  const [showSearch, setShowSearch] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const filteredLines = searchQuery
    ? lines.filter(l => l.message.toLowerCase().includes(searchQuery.toLowerCase()))
    : lines

  useEffect(() => {
    if (autoScroll && !searchQuery) bottomRef.current?.scrollIntoView({ behavior: 'instant' })
  }, [lines, autoScroll, searchQuery])

  useEffect(() => {
    if (showSearch) searchRef.current?.focus()
  }, [showSearch])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
        e.preventDefault()
        setShowSearch(v => {
          if (v) setSearchQuery('')
          return !v
        })
      }
    }
    globalThis.addEventListener('keydown', onKeyDown)
    return () => globalThis.removeEventListener('keydown', onKeyDown)
  }, [])

  function handleSearchKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { setShowSearch(false); setSearchQuery('') }
  }

  function closeSearch() { setShowSearch(false); setSearchQuery('') }

  function handleScroll() {
    const el = containerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setAutoScroll(atBottom)
  }

  return (
    <div className={cn('flex flex-col rounded-xl border overflow-hidden', 'bg-[var(--color-background)] border-[var(--color-border)] shadow-[var(--shadow-sm)]', className)}>
      {/* Toolbar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-[var(--color-border)] bg-[var(--color-surface)]">
        <div className="flex items-center gap-2 text-xs text-[var(--color-text-muted)]">
          <span
            className={cn(
              'inline-block w-1.5 h-1.5 rounded-full',
              connected ? 'bg-emerald-400 shadow-[0_0_6px_#34d399]' : 'bg-[var(--color-text-dim)]',
            )}
          />
          <span className="font-terminal">{connected ? 'live' : 'disconnected'}</span>
          <span className="text-[var(--color-text-dim)]">·</span>
          {searchQuery ? (
            <span className="text-amber-400 tabular-nums">{filteredLines.length} of {lines.length} lines</span>
          ) : (
            <span className="text-[var(--color-text-dim)]">{lines.length} lines</span>
          )}
          {currentJob && (
            <>
              <span className="text-[var(--color-text-dim)]">·</span>
              <span className="text-violet-400 truncate max-w-[260px]">
                {currentJob.company}, {currentJob.role}
              </span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => { setShowSearch(v => !v); if (showSearch) setSearchQuery('') }}
            className={cn(
              'text-xs px-2 py-0.5 rounded',
              showSearch
                ? 'bg-amber-500/15 text-amber-400'
                : 'text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)]',
            )}
            title="Search logs (Ctrl+F)"
          >
            search
          </button>
          <button
            onClick={() => setAutoScroll(v => !v)}
            className={cn(
              'text-xs px-2 py-0.5 rounded',
              autoScroll
                ? 'bg-violet-500/15 text-violet-400'
                : 'text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)]',
            )}
          >
            auto-scroll
          </button>
          <button
            onClick={onClear}
            className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] px-2 py-0.5 rounded hover:bg-[var(--color-surface-2)]"
          >
            clear
          </button>
        </div>
      </div>

      {/* Search bar */}
      {showSearch && (
        <div className="flex items-center gap-2 px-3 py-1.5 border-b border-[var(--color-border)] bg-[var(--color-surface)]">
          <svg className="shrink-0 w-3 h-3 text-[var(--color-text-dim)]" viewBox="0 0 16 16" fill="currentColor">
            <path d="M6.5 0a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zm0 1a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11zm4.78 9.72 3.5 3.5-.72.72-3.5-3.5.72-.72z"/>
          </svg>
          <input
            ref={searchRef}
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            onKeyDown={handleSearchKeyDown}
            placeholder="filter logs…"
            className="flex-1 bg-transparent text-xs font-terminal text-[var(--color-text-muted)] placeholder:text-[var(--color-text-dim)] outline-none"
          />
          {searchQuery && (
            <span className="text-xs text-amber-400 tabular-nums shrink-0">
              {filteredLines.length} {filteredLines.length === 1 ? 'match' : 'matches'}
            </span>
          )}
          <button
            onClick={closeSearch}
            className="shrink-0 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] leading-none"
            title="Close (Esc)"
          >
            ✕
          </button>
        </div>
      )}

      {/* Log area */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="font-terminal flex-1 overflow-y-auto p-3 space-y-0.5 min-h-0"
        style={{ maxHeight: '400px' }}
      >
        {filteredLines.length === 0 && (
          <div className="text-[var(--color-text-dim)] text-xs">
            {searchQuery ? 'No matching lines.' : 'Waiting for logs…'}
          </div>
        )}
        {filteredLines.map((line, idx) => (
          <div key={line.id}>
          {idx > 0 && line.message.includes(': processing,') && (
            <div className="my-1.5 border-t border-[var(--color-border)] opacity-40" />
          )}
          <div className="flex gap-2 leading-relaxed">
            <span className="shrink-0 text-[var(--color-text-dim)] tabular-nums text-[0.7rem]">
              {new Date(line.time).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
            </span>
            <span className={cn('shrink-0 uppercase text-[0.7rem] font-semibold w-8', getBadgeClass(line))}>
              {getBadgeLabel(line)}
            </span>
            <span
              className={cn('break-all', getMessageClass(line))}
              // eslint-disable-next-line react/no-danger
              dangerouslySetInnerHTML={{ __html: buildHighlightedHtml(line.message, searchQuery) }}
            />
          </div>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}
