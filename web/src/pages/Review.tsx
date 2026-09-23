import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import { ReviewCard } from '../components/review/ReviewCard'
import { PageHeader } from '../components/shell/PageHeader'
import { Button } from '../components/Button'
import { botApi } from '../api/bot'
import { settingsApi } from '../api/settings'

export function Review() {
  const qc = useQueryClient()
  const [index, setIndex] = useState(0)

  const { data: pending = [], isLoading } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 10000,
  })
  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const approve = useMutation({
    mutationFn: botApi.reviewApprove,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['review-pending'] })
      setIndex(i => Math.max(0, i - 1))
    },
  })
  const reject = useMutation({
    mutationFn: botApi.reviewReject,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['review-pending'] })
      setIndex(i => Math.max(0, i - 1))
    },
  })

  if (isLoading) {
    return <div className="text-center py-12 text-[var(--color-text-dim)] text-sm">Loading…</div>
  }

  if (pending.length === 0) {
    return (
      <div className="space-y-6">
        <PageHeader
          title="Review"
          description="Applications are only submitted after your approval."
        />
        <div className="rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-16 text-center space-y-3">
          <p className="text-base font-medium text-[var(--color-text)]">You&apos;re all caught up</p>
          <p className="text-sm text-[var(--color-text-dim)] max-w-md mx-auto">
            No applications need review right now. Jobifai will place applications here whenever your approval is required.
          </p>
          <Link to="/" className="inline-block text-sm font-medium text-[var(--color-accent)] hover:underline pt-2">
            Return to dashboard
          </Link>
        </div>
      </div>
    )
  }

  const safeIndex = Math.min(index, pending.length - 1)
  const review = pending[safeIndex]

  return (
    <div className="space-y-6">
      <PageHeader
        title="Review"
        description="Applications are only submitted after your approval."
        actions={
          <span className="text-sm font-medium text-[var(--color-text-muted)] tabular-nums">
            {pending.length} remaining
          </span>
        }
      />

      <div className="flex items-center justify-between gap-2 max-w-[800px] mx-auto">
        <Button
          variant="ghost"
          size="sm"
          disabled={safeIndex <= 0}
          leftIcon={<ChevronLeft size={14} />}
          onClick={() => setIndex(i => i - 1)}
        >
          Previous
        </Button>
        <span className="text-xs text-[var(--color-text-dim)] tabular-nums">
          {safeIndex + 1} of {pending.length}
        </span>
        <Button
          variant="ghost"
          size="sm"
          disabled={safeIndex >= pending.length - 1}
          leftIcon={<ChevronRight size={14} />}
          onClick={() => setIndex(i => i + 1)}
        >
          Next
        </Button>
      </div>

      <ReviewCard
        key={review.job_id}
        review={review}
        halalEnabled={halalEnabled}
        onApprove={() => approve.mutate(review.job_id)}
        onReject={() => reject.mutate(review.job_id)}
      />
    </div>
  )
}
