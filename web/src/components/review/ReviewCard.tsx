import { useEffect, useRef, useState } from 'react'
import { Check, X, FileText, ExternalLink } from 'lucide-react'
import { cn } from '../../lib'
import { getToken } from '../../api/client'
import type { PendingReview } from '../../types'
import { PlatformBadge } from '../PlatformBadge'
import { ScorePill } from '../ScorePill'
import { Button } from '../ui/button'
import { EthicsVerdict } from './EthicsVerdict'
import { JobMatchReasoning } from './JobMatchReasoning'

interface ReviewCardProps {
  readonly review: PendingReview
  readonly halalEnabled: boolean
  readonly onApprove: () => void
  readonly onReject: () => void
  readonly className?: string
}

export function ReviewCard({ review, halalEnabled, onApprove, onReject, className }: ReviewCardProps) {
  const touchStartX = useRef<number | null>(null)
  const [swipeDir, setSwipeDir] = useState<'approve' | 'reject' | null>(null)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (document.activeElement !== document.body && document.activeElement?.tagName !== 'BODY') return
      const tag = (document.activeElement as HTMLElement)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || tag === 'BUTTON') return
      if (e.key === 'a' || e.key === 'A') triggerApprove()
      if (e.key === 'r' || e.key === 'R') triggerReject()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onApprove, onReject])

  function triggerApprove() {
    setDismissed(true)
    setTimeout(onApprove, 200)
  }
  function triggerReject() {
    setDismissed(true)
    setTimeout(onReject, 200)
  }

  function onTouchStart(e: React.TouchEvent) {
    touchStartX.current = e.touches[0].clientX
  }
  function onTouchMove(e: React.TouchEvent) {
    if (touchStartX.current == null) return
    const dx = e.touches[0].clientX - touchStartX.current
    if (Math.abs(dx) > 30) setSwipeDir(dx > 0 ? 'approve' : 'reject')
    else setSwipeDir(null)
  }
  function onTouchEnd() {
    if (swipeDir === 'approve') triggerApprove()
    else if (swipeDir === 'reject') triggerReject()
    setSwipeDir(null)
    touchStartX.current = null
  }

  const isDoubtful = halalEnabled && review.halal_verdict?.verdict === 'DOUBTFUL'
  const hasScore = (review.suitability_score ?? 0) > 0

  return (
    <article
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      className={cn(
        'rounded-[var(--radius-xl)] border p-5 sm:p-6 transition-all duration-200 shadow-[var(--shadow-card)] max-w-[800px] mx-auto',
        isDoubtful ? 'border-[var(--color-warn)]/35 bg-[var(--color-warn-soft)]/40' : 'bg-[var(--color-surface)] border-[var(--color-border)]',
        swipeDir === 'approve' && 'border-[var(--color-success)]/45 translate-x-2',
        swipeDir === 'reject' && 'border-[var(--color-danger)]/45 -translate-x-2',
        dismissed && 'scale-[0.98] opacity-0',
        className,
      )}
    >
      <header className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3 mb-4">
        <div className="min-w-0 order-2 sm:order-1">
          <h2 className="text-lg font-semibold text-[var(--color-text)]">{review.company || '—'}</h2>
          <p className="text-sm text-[var(--color-text-muted)] mt-0.5">{review.role}</p>
          {review.location && (
            <p className="text-xs text-[var(--color-text-dim)] mt-1">{review.location}</p>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0 order-1 sm:order-2">
          {hasScore ? <ScorePill score={review.suitability_score!} showLabel /> : null}
          <PlatformBadge platform={review.platform} />
        </div>
      </header>

      <JobMatchReasoning reasoning={review.suitability_reasoning} className="mb-4" />

      {halalEnabled && review.halal_verdict && (
        <EthicsVerdict verdict={review.halal_verdict} className="mb-4" />
      )}

      {(review.resume_path || review.cover_letter_path) && (
        <div className="flex flex-wrap gap-3 mb-5">
          {review.resume_path && (
            <a
              href={`/api/files/${review.resume_path.replace(/^job_applications\//, '')}?token=${getToken() ?? ''}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 text-sm text-[var(--color-accent)] hover:underline"
            >
              <FileText size={14} />
              Resume preview
            </a>
          )}
          {review.cover_letter_path && (
            <a
              href={`/api/files/${review.cover_letter_path.replace(/^job_applications\//, '')}?token=${getToken() ?? ''}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 text-sm text-[var(--color-accent)] hover:underline"
            >
              <FileText size={14} />
              Cover letter
            </a>
          )}
        </div>
      )}

      {review.link && (
        <a
          href={review.link}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 text-sm text-[var(--color-text-muted)] hover:text-[var(--color-accent)] mb-6"
        >
          View original listing
          <ExternalLink size={14} />
        </a>
      )}

      <p className="text-xs text-[var(--color-text-dim)] mb-4 md:hidden">
        Swipe right to approve · Swipe left to reject
      </p>

      <div className="flex flex-col sm:flex-row gap-2 sm:justify-between sm:items-center sticky bottom-0 md:static bg-[var(--color-surface)] md:bg-transparent pt-2 md:pt-0 border-t md:border-t-0 border-[var(--color-border-subtle)] md:border-0 -mx-5 px-5 md:mx-0 md:px-0 pb-[env(safe-area-inset-bottom)] md:pb-0">
        <div className="hidden sm:flex text-xs text-[var(--color-text-dim)] gap-3">
          <span><kbd className="px-1.5 py-0.5 rounded border border-[var(--color-border)] bg-[var(--color-surface-2)]">R</kbd> Reject</span>
          <span><kbd className="px-1.5 py-0.5 rounded border border-[var(--color-border)] bg-[var(--color-surface-2)]">A</kbd> Approve</span>
        </div>
        <div className="flex gap-2 w-full sm:w-auto">
          <Button variant="danger" className="flex-1 sm:flex-none" leftIcon={<X size={14} />} onClick={triggerReject}>
            Reject
          </Button>
          <Button variant="primary" className="flex-1 sm:flex-none" leftIcon={<Check size={14} />} onClick={triggerApprove}>
            Approve application
          </Button>
        </div>
      </div>
    </article>
  )
}
