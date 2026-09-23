import { useState } from 'react'
import { ChevronDown, ExternalLink, FileText, Trash2 } from 'lucide-react'
import { cn, formatDate, relativeTime } from '../../lib'
import type { AppliedJob } from '../../types'
import { PlatformBadge } from '../PlatformBadge'
import { ScorePill } from '../ScorePill'
import { getToken } from '../../api/client'

interface Props {
  readonly job: AppliedJob
  readonly onDelete: (id: string) => void
  readonly deletePending: boolean
}

export function AppliedJobItem({ job, onDelete, deletePending }: Props) {
  const [open, setOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  return (
    <div className="border-b border-[var(--color-border-subtle)] last:border-0">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full text-left px-4 py-4 hover:bg-[var(--color-surface-2)]/80 transition-colors"
      >
        <div className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 sm:grid-cols-[2.75rem_minmax(0,1fr)_auto]">
          <span aria-hidden="true" className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] text-sm font-bold text-[var(--color-text-muted)] sm:h-11 sm:w-11">
            {(job.company || '?').trim().charAt(0).toUpperCase()}
          </span>
          <div className="flex-1 min-w-0">
            <p className="font-semibold text-[var(--color-text)] line-clamp-2">{job.role}</p>
            <p className="text-sm text-[var(--color-text-muted)] truncate">{job.company}{job.location ? ` · ${job.location}` : ''}</p>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 shrink-0">
            <PlatformBadge platform={job.platform} size="sm" />
            {job.suitability_score != null && job.suitability_score > 0 ? (
              <ScorePill score={job.suitability_score} compact />
            ) : null}
            <span className="text-xs text-[var(--color-text-dim)] tabular-nums" title={formatDate(job.applied_at)}>
              {relativeTime(job.applied_at)}
            </span>
            <ChevronDown size={14} className={cn('hidden text-[var(--color-text-dim)] transition-transform sm:block', open && 'rotate-180')} />
          </div>
        </div>
      </button>
      {open && (
        <div className="px-4 pb-4 pt-0 space-y-3 border-t border-[var(--color-border-subtle)] bg-[var(--color-surface-2)]/30">
          <p className="text-xs text-[var(--color-text-dim)] pt-3">
            Applied {formatDate(job.applied_at)}
          </p>
          <div className="flex flex-wrap gap-3">
            {job.resume_path && (
              <a
                href={`/api/files/${job.resume_path.replace(/^job_applications\//, '')}?token=${getToken() ?? ''}`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 text-xs text-[var(--color-accent)] hover:underline"
              >
                <FileText size={12} /> Resume
              </a>
            )}
            {job.cover_letter_path && (
              <a
                href={`/api/files/${job.cover_letter_path.replace(/^job_applications\//, '')}?token=${getToken() ?? ''}`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 text-xs text-[var(--color-accent)] hover:underline"
              >
                <FileText size={12} /> Cover letter
              </a>
            )}
            <a href={job.link} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-xs text-[var(--color-text-muted)] hover:text-[var(--color-accent)]">
              Application URL <ExternalLink size={11} />
            </a>
          </div>
          {confirmDelete ? (
            <span className="flex items-center gap-2 text-xs">
              <button type="button" onClick={() => onDelete(job.id)} disabled={deletePending} className="text-[var(--color-danger)] font-medium disabled:opacity-50">Delete record</button>
              <span className="text-[var(--color-text-dim)]">·</span>
              <button type="button" onClick={() => setConfirmDelete(false)} className="text-[var(--color-text-muted)]">Cancel</button>
            </span>
          ) : (
            <button type="button" onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-danger)]">
              <Trash2 size={12} /> Delete record
            </button>
          )}
        </div>
      )}
    </div>
  )
}
