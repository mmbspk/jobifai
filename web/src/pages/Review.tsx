import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Check, X, FileText } from 'lucide-react'
import { PlatformBadge } from '../components/PlatformBadge'
import { ScorePill } from '../components/ScorePill'
import { cn, relativeTime, formatDate } from '../lib'
import { botApi } from '../api/bot'
import { settingsApi } from '../api/settings'
import type { PendingReview } from '../types'

function ReviewCard({ review, halalEnabled, onApprove, onReject }: {
  review: PendingReview
  halalEnabled: boolean
  onApprove: () => void
  onReject: () => void
}) {
  const cardRef = useRef<HTMLDivElement>(null)
  const touchStartX = useRef<number | null>(null)
  const [swipeDir, setSwipeDir] = useState<'approve' | 'reject' | null>(null)
  const [dismissed, setDismissed] = useState(false)

  // Touch swipe handlers
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

  // Keyboard shortcuts
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (document.activeElement !== document.body) return
      if (e.key === 'a' || e.key === 'A') triggerApprove()
      if (e.key === 'r' || e.key === 'R') triggerReject()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  function triggerApprove() {
    setDismissed(true)
    setTimeout(onApprove, 250)
  }
  function triggerReject() {
    setDismissed(true)
    setTimeout(onReject, 250)
  }

  const isDoubtful = halalEnabled && review.halal_verdict?.verdict === 'DOUBTFUL'

  return (
    <div
      ref={cardRef}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      className={cn(
        'rounded-xl border p-4 transition-all duration-250',
        isDoubtful ? 'bg-orange-500/10 border-orange-500/30' : 'bg-[var(--color-surface)] border-[var(--color-border)]',
        swipeDir === 'approve' && 'border-emerald-500/50 bg-emerald-500/5 translate-x-2',
        swipeDir === 'reject' && 'border-red-500/50 bg-red-500/5 -translate-x-2',
        dismissed && 'scale-95 opacity-0',
      )}
    >
      <div className="flex items-center justify-between gap-2 mb-3">
        <div>
          <div className="font-medium text-[var(--color-text)]">{review.company || '—'}</div>
          <div className="text-sm text-[var(--color-text-muted)]">{review.role}</div>
          {review.location && (
            <div className="text-xs text-[var(--color-text-dim)] mt-0.5">{review.location}</div>
          )}
        </div>
        <div className="flex items-center gap-2">
          {(review.suitability_score ?? 0) > 0
            ? <ScorePill score={review.suitability_score!} />
            : <span className="text-xs text-[var(--color-text-dim)]">—</span>
          }
          <PlatformBadge platform={review.platform} />
        </div>
      </div>

      <div className="text-xs text-[var(--color-text-dim)] mb-4" title={formatDate(review.created_at)}>{relativeTime(review.created_at)}</div>

      {isDoubtful && review.halal_verdict && (
        <div className="mb-4 space-y-1">
          <p className="text-xs font-medium text-orange-300/80">
            Islamic ethics: DOUBTFUL ({review.halal_verdict.confidence} confidence)
          </p>
          <p className="text-xs text-[var(--color-text-muted)]">{review.halal_verdict.summary}</p>
          {review.halal_verdict.reasons.map(r => (
            <p key={r} className="text-xs text-[var(--color-text-dim)]">· {r}</p>
          ))}
        </div>
      )}

      {(review.resume_path || review.cover_letter_path) && (
        <div className="flex gap-3 mb-4">
          {review.resume_path && (
            <a
              href={`/api/files/${review.resume_path.replace(/^job_applications\//, '')}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 text-xs text-violet-400 hover:text-violet-300"
            >
              <FileText size={12} />
              Resume
            </a>
          )}
          {review.cover_letter_path && (
            <a
              href={`/api/files/${review.cover_letter_path.replace(/^job_applications\//, '')}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 text-xs text-violet-400 hover:text-violet-300"
            >
              <FileText size={12} />
              Cover Letter
            </a>
          )}
        </div>
      )}

      <div className="flex gap-2">
        <button
          onClick={triggerApprove}
          className="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-lg bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 text-sm font-medium transition-colors border border-emerald-500/20"
        >
          <Check size={14} />
          Approve
          <kbd className="hidden sm:inline text-[0.65rem] bg-emerald-500/10 px-1 rounded ml-1">A</kbd>
        </button>
        <button
          onClick={triggerReject}
          className="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-lg bg-red-500/10 text-red-400 hover:bg-red-500/20 text-sm font-medium transition-colors border border-red-500/20"
        >
          <X size={14} />
          Reject
          <kbd className="hidden sm:inline text-[0.65rem] bg-red-500/10 px-1 rounded ml-1">R</kbd>
        </button>
      </div>
    </div>
  )
}

export function Review() {
  const qc = useQueryClient()
  const { data: pending = [], isLoading } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 10000,
  })
  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const approve = useMutation({
    mutationFn: botApi.reviewApprove,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['review-pending'] }),
  })
  const reject = useMutation({
    mutationFn: botApi.reviewReject,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['review-pending'] }),
  })

  if (isLoading) return <div className="text-center py-12 text-[var(--color-text-dim)] text-sm">Loading…</div>

  if (pending.length === 0) {
    return (
      <div className="text-center py-16">
        <div className="text-[var(--color-text-dim)] mb-2">No pending reviews</div>
        <div className="text-xs text-[var(--color-text-dim)]">Applications requiring review will appear here</div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-sm font-medium text-[var(--color-text-muted)]">
          {pending.length} pending review
        </h1>
        <div className="text-xs text-[var(--color-text-dim)]">
          <span className="hidden sm:inline">Keyboard: </span>
          <kbd className="bg-[var(--color-surface-2)] border border-[var(--color-border)] px-1.5 py-0.5 rounded text-xs">A</kbd> approve ·{' '}
          <kbd className="bg-[var(--color-surface-2)] border border-[var(--color-border)] px-1.5 py-0.5 rounded text-xs">R</kbd> reject
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {pending.map(review => (
          <ReviewCard
            key={review.job_id}
            review={review}
            halalEnabled={halalEnabled}
            onApprove={() => approve.mutate(review.job_id)}
            onReject={() => reject.mutate(review.job_id)}
          />
        ))}
      </div>
    </div>
  )
}
