import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ChevronRight, CircleSlash2, ClipboardCheck, Send, Settings, Star } from 'lucide-react'
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
import { ScorePill } from '../components/ScorePill'
import { SetupGuide } from '../components/onboarding/SetupGuide'
import { useSetupReadiness } from '../hooks/useSetupReadiness'
import { useQuota } from '../hooks/useQuota'
import { quotaBlockMessage } from '../lib/quotaMessages'

export function Dashboard() {
  const navigate = useNavigate()
  const { user } = useAuth()
  const { status, start, stop, pause, resume, startError } = useBot()
  const setup = useSetupReadiness()
  const { aiDisabled, data: quota } = useQuota()
  const quotaHint = aiDisabled ? quotaBlockMessage(quota?.block_code, quota?.plan) : undefined
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
  const { data: topMatches = [] } = useQuery({
    queryKey: ['jobs-top-matches', 'dashboard-preview'],
    queryFn: () => jobsApi.topMatches({ limit: 3, offset: 0 }),
    staleTime: 60_000,
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

      <SetupGuide
        steps={setup.steps}
        ready={setup.ready}
        progress={setup.progress}
        nextStep={setup.nextStep}
        loading={setup.loading}
        onRefresh={setup.refresh}
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
            setupReady={setup.ready}
            setupHint={setup.nextStep?.title}
            aiDisabled={aiDisabled}
            quotaHint={quotaHint}
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

          <section className="overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-card)]">
            <div className="flex items-center justify-between gap-4 px-5 py-4">
              <div>
                <h2 className="text-base font-semibold text-[var(--color-text)]">Top matches</h2>
                <p className="mt-1 text-xs text-[var(--color-text-dim)]">The strongest roles from your latest search.</p>
              </div>
              <Link to="/jobs/top-matches" className="flex items-center gap-1 text-xs font-medium text-[var(--color-accent)] hover:underline">
                View all <ChevronRight size={14} />
              </Link>
            </div>
            <div className="divide-y divide-[var(--color-border-subtle)] border-t border-[var(--color-border-subtle)]">
              {topMatches.length > 0 ? topMatches.map(job => (
                <Link key={job.job_id} to="/jobs/top-matches" className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-5 py-4 transition-colors hover:bg-[var(--color-surface-2)]">
                  <span aria-hidden="true" className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] text-sm font-bold text-[var(--color-text-muted)]">
                    {(job.company || '?').trim().charAt(0).toUpperCase()}
                  </span>
                  <span className="min-w-0">
                    <strong className="block truncate text-sm font-semibold text-[var(--color-text)]">{job.role}</strong>
                    <span className="block truncate text-xs text-[var(--color-text-muted)]">{job.company}{job.location ? ` · ${job.location}` : ''}</span>
                  </span>
                  {job.suitability_score != null && job.suitability_score > 0
                    ? <ScorePill score={job.suitability_score} compact />
                    : <ChevronRight size={16} className="text-[var(--color-text-dim)]" />}
                </Link>
              )) : (
                <div className="px-5 py-8 text-center">
                  <p className="text-sm text-[var(--color-text-muted)]">No strong manual matches yet.</p>
                  <Link to="/generate" className="mt-2 inline-block text-xs font-medium text-[var(--color-accent)] hover:underline">Evaluate a role</Link>
                </div>
              )}
            </div>
          </section>

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
