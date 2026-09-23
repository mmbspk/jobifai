import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { CircleSlash2, ClipboardCheck, Send, Settings, Star } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { useBot } from '../hooks/useBot'
import { useLogs } from '../hooks/useLogs'
import { LogPanel } from '../components/LogPanel'
import { StatCard } from '../components/StatCard'
import { BotStatusCard } from '../components/automation/BotStatusCard'
import { PageHeader } from '../components/shell/PageHeader'
import { dashboardGreeting } from '../lib/dashboardGreeting'
import { Button } from '../components/Button'
import { jobsApi } from '../api/jobs'
import { botApi } from '../api/bot'
import type { Platform } from '../types'
import { useAuth } from '../contexts/AuthContext'

export function Dashboard() {
  const navigate = useNavigate()
  const { user } = useAuth()
  const { status, start, stop, pause, resume, startError } = useBot()
  const { lines, connected, clear } = useLogs()
  const [platform, setPlatform] = useState<Platform>('linkedin')

  const state = status?.state ?? 'idle'
  const isActive = state === 'running' || state === 'paused' || state === 'pending_review'
  const isRunning = state === 'running' || state === 'pending_review'
  const isPaused = state === 'paused'

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

  const pendingCount = pending?.length ?? 0
  const firstName = user?.display_name?.trim().split(/\s+/)[0]
  const greeting = `${dashboardGreeting()}${firstName ? `, ${firstName}` : ''}`

  const description = isRunning
    ? pendingCount > 0
      ? `Automation is working. ${pendingCount} application${pendingCount === 1 ? '' : 's'} need your review.`
      : 'Automation is working and your latest activity is shown below.'
    : pendingCount > 0
      ? `${pendingCount} application${pendingCount === 1 ? '' : 's'} need your review before submission.`
      : 'Your search, applications, and review controls are ready.'

  return (
    <div className="space-y-8">
      <PageHeader
        title={greeting}
        description={description}
        actions={
          <Button variant="secondary" size="sm" leftIcon={<Settings size={14} />} onClick={() => navigate('/settings/application')}>
            Settings
          </Button>
        }
      />

      <div className="xl:grid xl:grid-cols-[minmax(0,1fr)_360px] xl:gap-6 xl:items-start space-y-8 xl:space-y-0">
        <div className="space-y-8 min-w-0">
          <BotStatusCard
            status={status}
            state={state}
            platform={platform}
            onPlatformChange={setPlatform}
            startError={startError}
            pendingReviewCount={pendingCount}
            isActive={isActive}
            isRunning={isRunning}
            isPaused={isPaused}
            onStart={() => start.mutate(platform)}
            onStop={() => stop.mutate()}
            onPause={() => pause.mutate()}
            onResume={() => resume.mutate()}
            startPending={start.isPending}
            stopPending={stop.isPending}
            pausePending={pause.isPending}
            resumePending={resume.isPending}
          />

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <StatCard
              label="Applied today"
              value={stats?.applied_today ?? 0}
              sub="Submitted applications"
              icon={<Send size={18} />}
              tone="success"
              onClick={() => navigate('/jobs/applied?today=true')}
            />
            <StatCard
              label="Awaiting review"
              value={pendingCount}
              sub={pendingCount > 0 ? 'Ready for your decision' : 'Nothing waiting'}
              icon={<ClipboardCheck size={18} />}
              tone="accent"
              accent={pendingCount > 0}
              onClick={() => navigate('/review')}
            />
            <StatCard
              label="Top matches"
              value={stats?.top_matches_count ?? 0}
              sub="Strong roles to consider"
              icon={<Star size={18} />}
              tone="info"
              onClick={() => navigate('/jobs/top-matches')}
            />
            <StatCard
              label="Skipped today"
              value={stats?.skipped_today ?? 0}
              sub="Outside your criteria"
              icon={<CircleSlash2 size={18} />}
              tone="muted"
              onClick={() => navigate('/jobs/skipped?today=true')}
            />
          </div>

          {pendingCount > 0 && (
            <Link
              to="/review"
              className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-warn)]/30 bg-[var(--color-warn-soft)] px-4 py-3 hover:border-[var(--color-warn)]/45 transition-colors shadow-[var(--shadow-sm)]"
            >
              <ClipboardCheck size={18} className="text-[var(--color-warn)] shrink-0" />
              <span className="text-sm text-[var(--color-text)]">
                {pendingCount} application{pendingCount === 1 ? '' : 's'} need review
              </span>
              <span className="ml-auto text-xs text-[var(--color-text-muted)]">Review →</span>
            </Link>
          )}

          <div>
            <h2 className="text-sm font-semibold text-[var(--color-text)] mb-3">Quick actions</h2>
            <div className="grid sm:grid-cols-2 gap-3">
              <Link
                to="/review"
                className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-4 hover:border-[var(--color-accent)]/35 hover:bg-[var(--color-accent-soft)]/40 transition-all"
              >
                <ClipboardCheck size={18} className="text-[var(--color-accent)]" />
                <div>
                  <div className="text-sm font-medium text-[var(--color-text)]">Review applications</div>
                  <div className="text-xs text-[var(--color-text-dim)]">Approve before submission</div>
                </div>
              </Link>
              <Link
                to="/jobs/top-matches"
                className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-4 hover:border-[var(--color-accent)]/35 hover:bg-[var(--color-accent-soft)]/40 transition-all"
              >
                <Star size={18} className="text-[var(--color-accent)]" />
                <div>
                  <div className="text-sm font-medium text-[var(--color-text)]">Top matches</div>
                  <div className="text-xs text-[var(--color-text-dim)]">High-fit roles to apply manually</div>
                </div>
              </Link>
            </div>
          </div>

          <div className="xl:hidden">
            <ActivitySection lines={lines} connected={connected} onClear={clear} currentJob={status?.current_job} />
          </div>
        </div>

        <div className="hidden xl:block xl:sticky xl:top-6">
          <ActivitySection lines={lines} connected={connected} onClear={clear} currentJob={status?.current_job} />
        </div>
      </div>
    </div>
  )
}

function ActivitySection({
  lines, connected, onClear, currentJob,
}: {
  lines: Parameters<typeof LogPanel>[0]['lines']
  connected: boolean
  onClear: () => void
  currentJob: Parameters<typeof LogPanel>[0]['currentJob']
}) {
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-sm font-semibold text-[var(--color-text)]">Activity</h2>
        <span className="text-xs text-[var(--color-text-dim)] flex items-center gap-1.5">
          <span className={`w-1.5 h-1.5 rounded-full ${connected ? 'bg-[var(--color-success)]' : 'bg-[var(--color-text-dim)]'}`} />
          {connected ? 'Live' : 'Disconnected'}
        </span>
      </div>
      <LogPanel lines={lines} connected={connected} onClear={onClear} className="h-80 xl:h-[calc(100dvh-8rem)] xl:max-h-[640px]" currentJob={currentJob} />
    </div>
  )
}
