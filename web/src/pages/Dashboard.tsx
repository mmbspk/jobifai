import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Play, Square, Pause, RotateCcw, Zap, FileText, Star, Target, Mail } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { useBot } from '../hooks/useBot'
import { useLogs } from '../hooks/useLogs'
import { LogPanel } from '../components/LogPanel'
import { StatCard } from '../components/StatCard'
import { cn } from '../lib'
import { jobsApi } from '../api/jobs'
import { botApi } from '../api/bot'
import type { BotState, Platform } from '../types'

const PLATFORMS: { value: Platform; label: string }[] = [
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek',     label: 'Seek' },
]

const STATE_STYLE: Record<BotState, { ring: string; dot: string; label: string; pulse: boolean }> = {
  idle:           { ring: 'border-[var(--color-border)]',  dot: 'bg-[var(--color-text-dim)]',  label: 'Idle',           pulse: false },
  running:        { ring: 'border-violet-500',             dot: 'bg-violet-400',               label: 'Running',        pulse: true  },
  paused:         { ring: 'border-amber-500',              dot: 'bg-amber-400',                label: 'Paused',         pulse: false },
  pending_review: { ring: 'border-amber-500',              dot: 'bg-amber-400',                label: 'Pending Review', pulse: true  },
  stopped:        { ring: 'border-[var(--color-border)]',  dot: 'bg-[var(--color-text-dim)]',  label: 'Stopped',        pulse: false },
  error:          { ring: 'border-red-500',                dot: 'bg-red-400',                  label: 'Error',          pulse: false },
}

export function Dashboard() {
  const navigate = useNavigate()
  const { status, start, stop, pause, resume, startError } = useBot()
  const { lines, connected, clear } = useLogs()
  const [platform, setPlatform] = useState<Platform>('linkedin')

  const isActive = status?.state === 'running' || status?.state === 'paused' || status?.state === 'pending_review'

  const { data: stats } = useQuery({
    queryKey: ['jobs-stats'],
    queryFn: jobsApi.stats,
    staleTime: 10_000,
    refetchInterval: isActive ? 10_000 : false,
  })

  const { data: pending } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    staleTime: 30_000,
    refetchInterval: isActive ? 30_000 : false,
  })

  const state = status?.state ?? 'idle'
  const stateStyle = STATE_STYLE[state]
  const isRunning = state === 'running' || state === 'pending_review'
  const isPaused = state === 'paused'
  const todayCount = status?.today_count ?? 0
  const dailyLimit = status?.daily_limit ?? 0
  const progress = dailyLimit > 0 ? Math.min((todayCount / dailyLimit) * 100, 100) : 0

  return (
    <div className="space-y-6">
      {/* Hero status */}
      <div className={cn(
        'rounded-2xl border p-5 flex flex-col sm:flex-row items-start sm:items-center gap-4',
        'bg-[var(--color-surface)]',
        stateStyle.ring,
      )}>
        <div className="flex items-center gap-3 flex-1">
          <div className="relative flex items-center justify-center w-10 h-10">
            <div className={cn('w-3 h-3 rounded-full', stateStyle.dot)} />
            {stateStyle.pulse && (
              <div className={cn('absolute w-3 h-3 rounded-full animate-ping opacity-60', stateStyle.dot)} />
            )}
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-lg font-semibold text-[var(--color-text)]">{stateStyle.label}</span>
              {status?.platform && (
                <span className="text-sm text-[var(--color-text-dim)]">
                  · {status.platform.charAt(0).toUpperCase() + status.platform.slice(1)}
                  {status.keyword && ` · "${status.keyword}"`}
                  {status.location && ` · ${status.location}`}
                </span>
              )}
            </div>
            {status?.current_job && (
              <div className="text-sm text-[var(--color-text-muted)]">
                {status.current_job.company} · {status.current_job.role}
              </div>
            )}
            {status?.error && (
              <div className="text-sm text-[var(--color-danger)]">{status.error}</div>
            )}
            {startError && (
              <div className="text-sm text-[var(--color-danger)]">{startError}</div>
            )}
          </div>
        </div>

        {/* Progress */}
        {dailyLimit > 0 && (
          <div className="flex flex-col items-end gap-1">
            <div className="text-xs text-[var(--color-text-muted)]">{todayCount} / {dailyLimit} today</div>
            <div className="w-32 h-1.5 rounded-full bg-[var(--color-surface-2)]">
              <div
                className="h-full rounded-full bg-gradient-to-r from-violet-500 to-violet-400 transition-all"
                style={{ width: `${progress}%` }}
              />
            </div>
          </div>
        )}

        {/* Controls */}
        <div className="flex items-center gap-2">
          {!isActive && (
            <div className="flex gap-1">
              {PLATFORMS.map(p => (
                <button
                  key={p.value}
                  onClick={() => setPlatform(p.value)}
                  className={cn(
                    'px-2.5 py-1 rounded-lg text-xs font-medium transition-colors',
                    platform === p.value
                      ? 'bg-violet-500/20 text-violet-300 border border-violet-500/40'
                      : 'text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)]',
                  )}
                >
                  {p.label}
                </button>
              ))}
            </div>
          )}
          {isRunning && (
            <button
              onClick={() => pause.mutate()}
              disabled={pause.isPending}
              className={cn(
                'flex items-center gap-1.5 px-4 py-2 rounded-lg text-sm font-medium transition-all',
                'bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 border border-amber-500/30',
                pause.isPending && 'opacity-60 cursor-not-allowed',
              )}
            >
              <Pause size={14} fill="currentColor" />
              Pause
            </button>
          )}
          {isPaused && (
            <button
              onClick={() => resume.mutate()}
              disabled={resume.isPending}
              className={cn(
                'flex items-center gap-1.5 px-4 py-2 rounded-lg text-sm font-medium transition-all',
                'bg-violet-500 text-white hover:bg-violet-400 shadow-[0_0_16px_var(--color-accent-glow)]',
                resume.isPending && 'opacity-60 cursor-not-allowed',
              )}
            >
              <RotateCcw size={14} />
              Resume
            </button>
          )}
          <button
            onClick={() => (isActive ? stop.mutate() : start.mutate(platform))}
            disabled={start.isPending || stop.isPending}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2 rounded-lg text-sm font-medium transition-all',
              isActive
                ? 'bg-red-500/15 text-red-400 hover:bg-red-500/25 border border-red-500/30'
                : 'bg-violet-500 text-white hover:bg-violet-400 shadow-[0_0_16px_var(--color-accent-glow)]',
              (start.isPending || stop.isPending) && 'opacity-60 cursor-not-allowed',
            )}
          >
            {isActive ? <Square size={14} fill="currentColor" /> : <Play size={14} fill="currentColor" />}
            {isActive ? 'Stop' : 'Start'}
          </button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="Applied Today" value={stats?.applied_today ?? 0} icon={<Zap size={14} />} onClick={() => navigate('/jobs/applied?today=true')} />
        <StatCard label="Skipped Today" value={stats?.skipped_today ?? 0} onClick={() => navigate('/jobs/skipped?today=true')} />
        <StatCard label="Total Applied" value={stats?.total_applied ?? 0} onClick={() => navigate('/jobs/applied')} />
        <StatCard label="Total Skipped" value={stats?.total_skipped ?? 0} onClick={() => navigate('/jobs/skipped')} />
      </div>

      {/* Quick actions */}
      {(pending?.length ?? 0) > 0 && (
        <Link
          to="/review"
          className="flex items-center gap-3 rounded-xl border border-amber-500/30 bg-amber-500/5 px-4 py-3 hover:bg-amber-500/10 transition-colors"
        >
          <Star size={16} className="text-amber-400" />
          <span className="text-sm text-amber-300">
            {pending?.length} application{pending && pending.length > 1 ? 's' : ''} pending review
          </span>
          <span className="ml-auto text-amber-400/60 text-xs">Review →</span>
        </Link>
      )}

      <div className="grid grid-cols-4 gap-3">
        <Link to="/jobs/top-matches" className="flex items-center gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3 hover:border-violet-500/40 hover:bg-violet-500/5 transition-all group">
          <Star size={16} className="text-[var(--color-text-dim)] group-hover:text-violet-400 transition-colors" />
          <div>
            <div className="text-sm font-medium text-[var(--color-text)]">Top Matches</div>
            <div className="text-xs text-[var(--color-text-dim)]">Review your best-fit positions</div>
          </div>
        </Link>
        <Link to="/generate?tab=evaluate" className="flex items-center gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3 hover:border-violet-500/40 hover:bg-violet-500/5 transition-all group">
          <Target size={16} className="text-[var(--color-text-dim)] group-hover:text-violet-400 transition-colors" />
          <div>
            <div className="text-sm font-medium text-[var(--color-text)]">Job Fit</div>
            <div className="text-xs text-[var(--color-text-dim)]">Score how well a role matches your profile</div>
          </div>
        </Link>
        <Link to="/generate?tab=resume" className="flex items-center gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3 hover:border-violet-500/40 hover:bg-violet-500/5 transition-all group">
          <FileText size={16} className="text-[var(--color-text-dim)] group-hover:text-violet-400 transition-colors" />
          <div>
            <div className="text-sm font-medium text-[var(--color-text)]">Generate Resume</div>
            <div className="text-xs text-[var(--color-text-dim)]">Create a tailored or base resume</div>
          </div>
        </Link>
        <Link to="/generate?tab=cover" className="flex items-center gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3 hover:border-violet-500/40 hover:bg-violet-500/5 transition-all group">
          <Mail size={16} className="text-[var(--color-text-dim)] group-hover:text-violet-400 transition-colors" />
          <div>
            <div className="text-sm font-medium text-[var(--color-text)]">Generate Cover Letter</div>
            <div className="text-xs text-[var(--color-text-dim)]">AI-written cover letter for any role</div>
          </div>
        </Link>
      </div>

      {/* Live logs */}
      <div>
        <h2 className="text-sm font-medium text-[var(--color-text-muted)] mb-2">Live Logs</h2>
        <LogPanel lines={lines} connected={connected} onClear={clear} className="h-80" currentJob={status?.current_job} />
      </div>
    </div>
  )
}
