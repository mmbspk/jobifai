import { useEffect, useRef, useState } from 'react'
import { cn } from '../lib'
import type { LogLine, LogLevel } from '../hooks/useLogs'

const LEVEL_STYLE: Record<LogLevel, string> = {
  debug:    'text-zinc-500',
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

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

interface Props {
  lines: LogLine[]
  connected: boolean
  onClear: () => void
  className?: string
  currentJob?: { company: string; role: string } | null
}

export function LogPanel({ lines, connected, onClear, className, currentJob }: Props) {
  const [autoScroll, setAutoScroll] = useState(true)
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (autoScroll) bottomRef.current?.scrollIntoView({ behavior: 'instant' })
  }, [lines, autoScroll])

  function handleScroll() {
    const el = containerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setAutoScroll(atBottom)
  }

  return (
    <div className={cn('flex flex-col rounded-xl border overflow-hidden', 'bg-[var(--color-background)] border-[var(--color-border)]', className)}>
      {/* Toolbar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-[var(--color-border)] bg-[var(--color-surface)]">
        <div className="flex items-center gap-2 text-xs text-[var(--color-text-muted)]">
          <span
            className={cn(
              'inline-block w-1.5 h-1.5 rounded-full',
              connected ? 'bg-emerald-400 shadow-[0_0_6px_#34d399]' : 'bg-zinc-600',
            )}
          />
          <span className="font-terminal">{connected ? 'live' : 'disconnected'}</span>
          <span className="text-[var(--color-text-dim)]">·</span>
          <span className="text-[var(--color-text-dim)]">{lines.length} lines</span>
          {currentJob && (
            <>
              <span className="text-[var(--color-text-dim)]">·</span>
              <span className="text-violet-400 truncate max-w-[260px]">
                {currentJob.company} — {currentJob.role}
              </span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
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

      {/* Log area */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="font-terminal flex-1 overflow-y-auto p-3 space-y-0.5 min-h-0"
        style={{ maxHeight: '400px' }}
      >
        {lines.length === 0 && (
          <div className="text-[var(--color-text-dim)] text-xs">Waiting for logs…</div>
        )}
        {lines.map((line, i) => (
          <div key={i} className="flex gap-2 leading-relaxed">
            <span className="shrink-0 text-[var(--color-text-dim)] tabular-nums text-[0.7rem]">
              {new Date(line.time).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
            </span>
            <span className={cn('shrink-0 uppercase text-[0.7rem] font-semibold w-8', LEVEL_STYLE[line.level])}>
              {LEVEL_PREFIX[line.level]}
            </span>
            <span
              className="break-all text-[var(--color-text-muted)]"
              // eslint-disable-next-line react/no-danger
              dangerouslySetInnerHTML={{ __html: escapeHtml(line.message) }}
            />
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}
