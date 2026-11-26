import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Bell } from 'lucide-react'
import { Link } from 'react-router-dom'
import { settingsApi } from '../../api/settings'
import { useApplicationAnswerReviews } from '../../hooks/useApplicationAnswerReviews'

export function ApplicationAnswerNotice({ enabled = true }: { enabled?: boolean }) {
  const qc = useQueryClient()
  const reviews = useApplicationAnswerReviews(enabled)
  const read = useMutation({ mutationFn: settingsApi.applicationAnswers.markRead, onSuccess: () => { void qc.invalidateQueries({ queryKey: ['application-answer-reviews'] }) } })
  const counts = reviews.data?.pages[0]
  if (!enabled || !counts?.unread_count || !counts.unresolved_count) return null
  return <div className="mb-5 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3 text-sm"><span className="flex items-center gap-2"><Bell size={16} aria-hidden className="text-[var(--color-accent)]" />{counts.unresolved_count} application {counts.unresolved_count === 1 ? 'answer needs' : 'answers need'} confirmation</span><Link to="/settings/resume?view=application-answers&answers=confirmation" onClick={() => read.mutate()} className="inline-flex min-h-11 items-center text-[var(--color-accent)] hover:underline focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]">Review answers<span className="ml-2 rounded-full bg-[var(--color-accent-soft)] px-2 py-0.5 text-xs">{counts.unread_count} new</span></Link></div>
}
