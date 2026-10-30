import { useState } from 'react'
import { CheckCheck, ChevronDown, ExternalLink, Play, Trash2 } from 'lucide-react'
import { cn, formatDate, relativeTime } from '../../lib'
import type { SkippedJob } from '../../types'
import { PlatformBadge } from '../PlatformBadge'
import { ScorePill } from '../ScorePill'
import { EthicsVerdict } from '../review/EthicsVerdict'
import { JobMatchReasoning } from '../review/JobMatchReasoning'
import { Button } from '../ui/button'

interface Props {
  readonly job: SkippedJob
  readonly halalEnabled: boolean
  readonly onRetry: (id: string) => void
  readonly onMarkApplied: (id: string) => void
  readonly onDelete: (id: string) => void
  readonly actionPending: boolean
}

export function CannotApplyJobItem({
  job, halalEnabled, onRetry, onMarkApplied, onDelete, actionPending,
}: Props) {
  const [open, setOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const expandable = !!(job.suitability_reasoning || (halalEnabled && job.halal_verdict))
  const submissionUnknown = /submission_unknown|submission status is unknown|could not be confirmed/i.test(job.skip_reason)
  const failureMessage = job.skip_reason.replace(/^(easy apply|seek apply|quick apply):\s*(\[[^\]]+\]\s*)?/i, '')

  return (
    <div className="border-b border-[var(--color-border-subtle)] last:border-0">
      <div className="px-4 py-4">
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-start gap-3">
            <span aria-hidden="true" className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] text-sm font-bold text-[var(--color-text-muted)]">
              {(job.company || '?').trim().charAt(0).toUpperCase()}
            </span>
            <div className="min-w-0">
              <p className="font-semibold text-[var(--color-text)]">{job.role}</p>
              <p className="text-sm text-[var(--color-text-muted)]">{job.company}{job.location ? ` · ${job.location}` : ''}</p>
              <p className="text-xs font-medium text-[var(--color-warn)] mt-2">{submissionUnknown ? 'Check submission status' : 'Automation needs attention'}</p>
              <p className="text-xs text-[var(--color-text-dim)] mt-1">{failureMessage}</p>
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <PlatformBadge platform={job.platform} size="sm" />
              {job.suitability_score != null ? (
                <ScorePill score={job.suitability_score} compact />
              ) : null}
              <span className="text-xs text-[var(--color-text-dim)]" title={formatDate(job.viewed_at)}>{relativeTime(job.viewed_at)}</span>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" size="sm" leftIcon={<ExternalLink size={12} />} onClick={() => window.open(job.link, '_blank', 'noopener,noreferrer')}>
              Continue manually
            </Button>
            <Button variant="ghost" size="sm" leftIcon={<CheckCheck size={12} />} disabled={actionPending} onClick={() => onMarkApplied(job.id)}>
              Mark applied
            </Button>
            <Button variant="ghost" size="sm" leftIcon={<Play size={12} />} disabled={actionPending || submissionUnknown} onClick={() => onRetry(job.id)} title="Recheck the listing and suitability before applying">
              Re-evaluate
            </Button>
            {confirmDelete ? (
              <span className="flex items-center gap-1 text-xs self-center">
                <button type="button" disabled={actionPending} onClick={() => onDelete(job.id)} className="text-[var(--color-danger)] font-medium disabled:opacity-50">
                  Delete
                </button>
                <button type="button" onClick={() => setConfirmDelete(false)} className="text-[var(--color-text-muted)]">
                  Cancel
                </button>
              </span>
            ) : (
              <Button
                variant="ghost"
                size="sm"
                leftIcon={<Trash2 size={12} />}
                disabled={actionPending}
                onClick={() => setConfirmDelete(true)}
              >
                Delete
              </Button>
            )}
          </div>

          {expandable && (
            <button type="button" onClick={() => setOpen(v => !v)} className="flex items-center gap-1 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]">
              <ChevronDown size={12} className={cn(open && 'rotate-180')} />
              Score details
            </button>
          )}
        </div>
      </div>
      {open && expandable && (
        <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)] bg-[var(--color-surface-2)]/30">
          <JobMatchReasoning reasoning={job.suitability_reasoning} />
          {halalEnabled && job.halal_verdict && <EthicsVerdict verdict={job.halal_verdict} />}
        </div>
      )}
    </div>
  )
}
