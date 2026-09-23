import { Link } from 'react-router-dom'
import { Pause, Play, RotateCcw, Square, Zap } from 'lucide-react'
import { cn } from '../../lib'
import { Button } from '../ui/button'
import { Badge } from '../ui/badge'
import { DailyProgress } from './DailyProgress'
import type { BotState, BotStatus, Platform } from '../../types'

const PLATFORMS: { value: Platform; label: string }[] = [
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
]

interface BotStatusCardProps {
  readonly status: BotStatus | undefined
  readonly state: BotState
  readonly platform: Platform
  readonly onPlatformChange: (p: Platform) => void
  readonly startError: string | null
  readonly pendingReviewCount: number
  readonly isActive: boolean
  readonly isRunning: boolean
  readonly isPaused: boolean
  readonly onStart: () => void
  readonly onStop: () => void
  readonly onPause: () => void
  readonly onResume: () => void
  readonly startPending: boolean
  readonly stopPending: boolean
  readonly pausePending: boolean
  readonly resumePending: boolean
}

function stateLabel(state: BotState): string {
  switch (state) {
    case 'idle': return 'Ready'
    case 'running': return 'Running'
    case 'paused': return 'Paused'
    case 'pending_review': return 'Pending review'
    case 'stopped': return 'Stopped'
    case 'error': return 'Needs attention'
    default: return state
  }
}

function stateBadgeVariant(state: BotState): 'accent' | 'warn' | 'danger' | 'muted' {
  if (state === 'running') return 'accent'
  if (state === 'paused' || state === 'pending_review') return 'warn'
  if (state === 'error') return 'danger'
  return 'muted'
}

function stateHeadline(state: BotState): string {
  switch (state) {
    case 'idle': return 'Automation is ready'
    case 'running': return 'Searching for your next strong match'
    case 'paused': return 'Automation is paused'
    case 'pending_review': return 'Waiting for your review'
    case 'error': return 'Automation needs attention'
    case 'stopped': return 'Automation has stopped'
    default: return stateLabel(state)
  }
}

export function BotStatusCard(props: BotStatusCardProps) {
  const {
    status, state, platform, onPlatformChange, startError, pendingReviewCount,
    isActive, isRunning, isPaused,
    onStart, onStop, onPause, onResume,
    startPending, stopPending, pausePending, resumePending,
  } = props

  const pulse = state === 'running' || state === 'pending_review'
  const todayCount = status?.today_count ?? 0
  const dailyLimit = status?.daily_limit ?? 0

  return (
    <section
      className={cn(
        'relative overflow-hidden rounded-[var(--radius-xl)] border p-5 sm:p-6',
        'bg-[linear-gradient(125deg,var(--color-surface),var(--color-surface)_60%,var(--color-accent-soft))] shadow-[var(--shadow-card)]',
        state === 'error' && 'border-[var(--color-danger)]/40',
        (state === 'running' || state === 'pending_review') && 'border-[var(--color-accent)]/35',
        state === 'paused' && 'border-[var(--color-warn)]/35',
        !isActive && state !== 'error' && 'border-[var(--color-border)]',
      )}
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="relative flex h-11 w-11 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
              <Zap size={20} />
              {pulse && (
                <div
                  className={cn(
                    'absolute right-0 top-0 h-2.5 w-2.5 rounded-full animate-ping opacity-50',
                    state === 'running' ? 'bg-[var(--color-accent)]' : 'bg-[var(--color-warn)]',
                  )}
                />
              )}
              {pulse && (
                <div className={cn(
                  'absolute right-0 top-0 h-2.5 w-2.5 rounded-full border-2 border-[var(--color-surface)]',
                  state === 'running' ? 'bg-[var(--color-accent)]' : 'bg-[var(--color-warn)]',
                )} />
              )}
            </div>
            <div className="min-w-0">
              <div className="mb-1 flex flex-wrap items-center gap-2">
                <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-[var(--color-text)]">
                {stateHeadline(state)}
              </h2>
              {state === 'idle' && (
                <p className="text-sm text-[var(--color-text-dim)] mt-1">
                  Choose a platform when your profile and preferences are ready.
                </p>
              )}
              {status?.platform && isActive && (
                <p className="text-sm text-[var(--color-text-muted)] mt-1">
                  {status.platform.charAt(0).toUpperCase() + status.platform.slice(1)}
                  {status.keyword && ` · ${status.keyword}`}
                  {status.location && ` · ${status.location}`}
                </p>
              )}
            </div>
          </div>
          {status?.platform && isActive && <Badge variant="muted" className="capitalize">{status.platform}</Badge>}
        </div>

        {status?.current_job && isActive && (
          <div className="rounded-[var(--radius-md)] bg-[var(--color-surface-2)] px-4 py-3 border border-[var(--color-border-subtle)]">
            <p className="text-xs text-[var(--color-text-dim)] mb-0.5">Current role</p>
            <p className="text-sm font-medium text-[var(--color-text)]">{status.current_job.role}</p>
            <p className="text-sm text-[var(--color-text-muted)]">{status.current_job.company}</p>
          </div>
        )}

        {state === 'pending_review' && pendingReviewCount > 0 && (
          <p className="text-sm text-[var(--color-text-muted)]">
            {pendingReviewCount} application{pendingReviewCount === 1 ? '' : 's'} need your review before submission.
            {' '}
            <Link to="/review" className="text-[var(--color-accent)] hover:underline font-medium">
              Review applications
            </Link>
          </p>
        )}

        {(status?.error || startError) && (
          <div className="rounded-[var(--radius-md)] border border-[var(--color-danger)]/30 bg-[var(--color-danger-soft)] px-4 py-3">
            <p className="text-sm font-medium text-[var(--color-danger)]">Automation needs attention</p>
            <p className="text-sm text-[var(--color-text-muted)] mt-1">{status?.error ?? startError}</p>
          </div>
        )}

        {isPaused && (
          <p className="text-sm text-[var(--color-text-muted)]">
            Automation is paused. Your progress has been preserved.
          </p>
        )}

        <div className="flex flex-col sm:flex-row sm:items-end gap-4 justify-between">
          <DailyProgress count={todayCount} limit={dailyLimit} />

          <div className="flex flex-wrap items-center gap-2 justify-end">
            {!isActive && (
              <div className="flex gap-1 mr-1">
                {PLATFORMS.map(p => (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => onPlatformChange(p.value)}
                    className={cn(
                      'px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-medium transition-colors min-h-[44px] sm:min-h-0',
                      platform === p.value
                        ? 'bg-[var(--color-accent-soft)] text-[var(--color-accent)] border border-[var(--color-accent)]/35'
                        : 'text-[var(--color-text-dim)] hover:text-[var(--color-text)] hover:bg-[var(--color-surface-2)] border border-transparent',
                    )}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            )}
            {isRunning && (
              <Button variant="secondary" size="md" loading={pausePending} leftIcon={<Pause size={14} />} onClick={onPause}>
                Pause
              </Button>
            )}
            {isPaused && (
              <Button variant="primary" size="md" loading={resumePending} leftIcon={<RotateCcw size={14} />} onClick={onResume}>
                Resume
              </Button>
            )}
            <Button
              variant={isActive ? 'danger' : 'primary'}
              size="md"
              loading={isActive ? stopPending : startPending}
              leftIcon={isActive ? <Square size={14} fill="currentColor" /> : <Play size={14} fill="currentColor" />}
              onClick={isActive ? onStop : onStart}
            >
              {isActive ? 'Stop automation' : 'Start automation'}
            </Button>
          </div>
        </div>
      </div>
      {pulse && (
        <div className="absolute inset-x-0 bottom-0 h-px overflow-hidden">
          <span className="block h-full w-1/3 animate-[status-scan_2.4s_ease-in-out_infinite] bg-gradient-to-r from-transparent via-[var(--color-accent)] to-transparent" />
        </div>
      )}
    </section>
  )
}
