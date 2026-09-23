import { useState } from 'react'
import { Ban, CheckCheck, ChevronDown, ExternalLink, Trash2, X } from 'lucide-react'
import { cn, formatPostedDisplay } from '../../lib'
import type { PendingReview } from '../../types'
import { PlatformBadge } from '../PlatformBadge'
import { ScorePill } from '../ScorePill'
import { EthicsVerdict } from '../review/EthicsVerdict'
import { JobMatchReasoning } from '../review/JobMatchReasoning'
import { Button } from '../ui/button'

interface Props {
  readonly job: PendingReview
  readonly halalEnabled: boolean
  readonly onMarkApplied: (jobId: string) => void
  readonly onDelete: (jobId: string) => void
  readonly onBlacklist: (jobId: string) => void
  readonly onBlacklistAndClear: (job: PendingReview) => void
  readonly pending: boolean
}

export function TopMatchJobItem({
  job, halalEnabled, onMarkApplied, onDelete, onBlacklist, onBlacklistAndClear, pending,
}: Props) {
  const [open, setOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [confirmBlacklist, setConfirmBlacklist] = useState(false)
  const isDoubtful = halalEnabled && job.halal_verdict?.verdict === 'DOUBTFUL'
  const expandable = !!(job.suitability_reasoning || isDoubtful)
  const posted = formatPostedDisplay(job.posted_date, job.created_at)

  return (
    <div className={cn(
      'border-b border-[var(--color-border-subtle)] last:border-0',
      isDoubtful && !open && 'bg-[var(--color-warn-soft)]/25',
    )}>
      <div className="px-4 py-4">
        <div className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-start gap-3">
          <span aria-hidden="true" className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] text-sm font-bold text-[var(--color-text-muted)]">
            {(job.company || '?').trim().charAt(0).toUpperCase()}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2 mb-1">
              {job.suitability_score != null && job.suitability_score > 0 ? (
                <ScorePill score={job.suitability_score} showLabel />
              ) : null}
              {job.easy_apply && (
                <span className="text-[0.65rem] text-[var(--color-warn)] font-medium">Easy Apply</span>
              )}
            </div>
            <p className="font-semibold text-[var(--color-text)]">{job.role}</p>
            <p className="text-sm text-[var(--color-text-muted)]">{job.company}</p>
            {job.location && <p className="text-xs text-[var(--color-text-dim)] mt-0.5">{job.location}</p>}
            <p className="text-xs text-[var(--color-text-dim)] mt-1" title={posted.title}>{posted.label}</p>
          </div>
          <PlatformBadge platform={job.platform} size="sm" />
        </div>

        {expandable && (
          <button type="button" onClick={() => setOpen(v => !v)} className="mt-3 flex items-center gap-1 text-xs text-[var(--color-accent)] hover:underline">
            <ChevronDown size={12} className={cn(open && 'rotate-180')} />
            Why it matches
          </button>
        )}

        <div className="flex flex-wrap gap-2 mt-4">
          {job.link && (
            <Button variant="secondary" size="sm" leftIcon={<ExternalLink size={12} />} onClick={() => window.open(job.link, '_blank', 'noopener,noreferrer')}>
              View role
            </Button>
          )}
          <Button variant="primary" size="sm" leftIcon={<CheckCheck size={12} />} disabled={pending} onClick={() => onMarkApplied(job.job_id)}>
            Mark applied
          </Button>
          {confirmBlacklist ? (
            <span className="flex items-center gap-1 text-xs">
              <button type="button" disabled={pending} onClick={() => onBlacklist(job.job_id)} className="text-[var(--color-warn)]">Blacklist</button>
              <button type="button" disabled={pending} onClick={() => onBlacklistAndClear(job)} className="text-[var(--color-danger)]">+ remove all</button>
              <button type="button" onClick={() => setConfirmBlacklist(false)}><X size={12} /></button>
            </span>
          ) : (
            <Button variant="ghost" size="sm" leftIcon={<Ban size={12} />} onClick={() => { setConfirmDelete(false); setConfirmBlacklist(true) }}>
              Blacklist
            </Button>
          )}
          {confirmDelete ? (
            <span className="flex items-center gap-1 text-xs">
              <button type="button" disabled={pending} onClick={() => onDelete(job.job_id)} className="text-[var(--color-danger)]">Delete</button>
              <button type="button" onClick={() => setConfirmDelete(false)}>Cancel</button>
            </span>
          ) : (
            <Button variant="ghost" size="sm" leftIcon={<Trash2 size={12} />} onClick={() => { setConfirmBlacklist(false); setConfirmDelete(true) }}>
              Hide
            </Button>
          )}
        </div>
      </div>

      {open && expandable && (
        <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)]">
          <JobMatchReasoning reasoning={job.suitability_reasoning} />
          {isDoubtful && job.halal_verdict && <EthicsVerdict verdict={job.halal_verdict} />}
        </div>
      )}
    </div>
  )
}
