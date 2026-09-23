import { useState } from 'react'
import { ChevronDown, ExternalLink, Trash2 } from 'lucide-react'
import { cn, formatDate, relativeTime } from '../../lib'
import type { SkippedJob } from '../../types'
import { PlatformBadge } from '../PlatformBadge'
import { ScorePill } from '../ScorePill'
import { EthicsVerdict } from '../review/EthicsVerdict'
import { JobMatchReasoning } from '../review/JobMatchReasoning'

interface Props {
  readonly job: SkippedJob
  readonly halalEnabled: boolean
  readonly onDelete: (id: string) => void
  readonly deletePending: boolean
}

export function SkippedJobItem({ job, halalEnabled, onDelete, deletePending }: Props) {
  const [open, setOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const expandable = !!(job.suitability_reasoning || (halalEnabled && job.halal_verdict))

  return (
    <div className="border-b border-[var(--color-border-subtle)] last:border-0">
      <button
        type="button"
        onClick={() => expandable && setOpen(v => !v)}
        className={cn(
          'w-full text-left px-4 py-4 transition-colors',
          expandable ? 'hover:bg-[var(--color-surface-2)]/80 cursor-pointer' : '',
        )}
      >
        <div className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-start gap-3">
          <span aria-hidden="true" className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] text-sm font-bold text-[var(--color-text-muted)]">
            {(job.company || '?').trim().charAt(0).toUpperCase()}
          </span>
          <div className="flex-1 min-w-0">
            <p className="font-semibold text-[var(--color-text)]">{job.role}</p>
            <p className="text-sm text-[var(--color-text-muted)]">{job.company}{job.location ? ` · ${job.location}` : ''}</p>
            <p className="text-xs text-[var(--color-warn)] mt-2 font-medium">Skipped</p>
            <p className="text-xs text-[var(--color-text-dim)] mt-1 line-clamp-2">{job.skip_reason}</p>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 shrink-0">
            <PlatformBadge platform={job.platform} size="sm" />
            {job.suitability_score != null && job.suitability_score > 0 ? (
              <ScorePill score={job.suitability_score} compact />
            ) : null}
            <span className="text-xs text-[var(--color-text-dim)]" title={formatDate(job.viewed_at)}>{relativeTime(job.viewed_at)}</span>
            {expandable && <ChevronDown size={14} className={cn('text-[var(--color-text-dim)]', open && 'rotate-180')} />}
          </div>
        </div>
      </button>
      {open && expandable && (
        <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)] bg-[var(--color-surface-2)]/30">
          <JobMatchReasoning reasoning={job.suitability_reasoning} />
          {halalEnabled && job.halal_verdict && <EthicsVerdict verdict={job.halal_verdict} />}
          <a href={job.link} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-xs text-[var(--color-accent)] hover:underline">
            View listing <ExternalLink size={11} />
          </a>
          {confirmDelete ? (
            <span className="flex items-center gap-2 text-xs">
              <button type="button" onClick={() => onDelete(job.id)} disabled={deletePending} className="text-[var(--color-danger)] font-medium">Delete</button>
              <button type="button" onClick={() => setConfirmDelete(false)} className="text-[var(--color-text-muted)]">Cancel</button>
            </span>
          ) : (
            <button type="button" onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-danger)]">
              <Trash2 size={12} /> Delete
            </button>
          )}
        </div>
      )}
    </div>
  )
}
