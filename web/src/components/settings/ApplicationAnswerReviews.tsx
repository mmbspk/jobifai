import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { Link } from 'react-router-dom'
import { settingsApi, type ApplicationAnswerReview } from '../../api/settings'
import { ApiError } from '../../api/client'
import { Button } from '../Button'
import { TextField } from './ApplicationDefaultsEditor'

export function ApplicationAnswerReviewRow({ item }: { item: ApplicationAnswerReview }) {
  const qc = useQueryClient()
  const original = item.suggested_answer ?? item.answer_used ?? ''
  const [editing, setEditing] = useState(false)
  const [answer, setAnswer] = useState(original)
  const [editVersion, setEditVersion] = useState({ revision: item.revision, profile_revision: item.profile_revision })
  const save = useMutation({
    mutationFn: ({ value, version }: { value: string; version: typeof editVersion }) => settingsApi.applicationAnswers.confirm(item.id, { answer: value, revision: version.revision, ...(version.profile_revision && { profile_revision: version.profile_revision }) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['application-answer-reviews'] })
      void qc.invalidateQueries({ queryKey: ['application-answers'] })
      void qc.invalidateQueries({ queryKey: ['application-answer-review', item.id] })
    },
  })
  const dismiss = useMutation({ mutationFn: () => settingsApi.applicationAnswers.dismiss(item.id, item.revision), onSuccess: () => { void qc.invalidateQueries({ queryKey: ['application-answer-reviews'] }); void qc.invalidateQueries({ queryKey: ['application-answer-review', item.id] }) } })
  const refresh = useMutation({
    mutationFn: () => settingsApi.applicationAnswers.review(item.id),
    onSuccess: async fresh => {
      setEditVersion({ revision: fresh.revision, profile_revision: fresh.profile_revision })
      qc.setQueryData(['application-answer-review', item.id], fresh)
      await qc.invalidateQueries({ queryKey: ['application-answer-reviews'] })
      save.reset()
    },
  })
  const conflict = save.isError && save.error instanceof ApiError && save.error.status === 409
  const actionable = !item.lifecycle || item.lifecycle === 'active'
  const state = !actionable ? { resolved: 'Confirmed for future use', dismissed: 'Review dismissed', outdated: 'Saved facts changed since this review', superseded: 'A newer decision replaced this review' }[item.lifecycle as 'resolved'] || 'Previous review' : item.status === 'required' ? 'One application needs this fact' : item.status === 'used' ? item.use_status === 'submitted' ? 'Answer submitted — confirm for future use' : item.use_status === 'submission_unknown' ? 'Answer filled — submission outcome unknown' : item.use_status === 'filled' ? 'Answer filled — submission not confirmed' : 'Answer used — confirm for future use' : 'Suggestion — not used in an application'
  return <article className="space-y-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-5">
    <p className="text-xs font-medium text-[var(--color-text-dim)]">{state}</p>
    <h3 className="text-sm font-semibold">{item.question}</h3>
    {item.use_status === 'replaced' && <p className="text-xs text-[var(--color-text-dim)]">This answer was filled, then replaced before submission.</p>}
    {item.answer_used && <div><p className="text-xs text-[var(--color-text-dim)]">Answer actually used</p><p className="mt-1 whitespace-pre-wrap break-words text-sm">{item.answer_used}</p></div>}
    {item.suggested_answer && item.suggested_answer !== item.answer_used && <div><p className="text-xs text-[var(--color-text-dim)]">Suggested answer for future use</p><p className="mt-1 whitespace-pre-wrap break-words text-sm">{item.suggested_answer}</p></div>}
    {item.status === 'required' && <p className="text-sm text-[var(--color-text-dim)]">Supply your answer for future use. Other eligible applications can continue.</p>}
    <p className="break-words text-xs text-[var(--color-text-dim)]">{[item.job_title, item.employer, item.country, item.platform].filter(Boolean).join(' · ') || 'This question’s application context'}{(item.occurrence_count ?? 0) > 1 ? ` · ${item.occurrence_count} equivalent questions` : ''}</p>
    {item.evidence?.length ? <details className="text-xs text-[var(--color-text-dim)]"><summary className="min-h-8 cursor-pointer text-[var(--color-accent)] focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]">View context and evidence</summary><ul className="list-disc space-y-1 pl-4">{item.evidence.map((e, i) => <li key={i}>{e}</li>)}</ul></details> : null}
    {actionable && (editing ? <div className="space-y-3"><TextField label="Answer for future use" multiline value={answer} onChange={setAnswer} /><div className="flex flex-wrap gap-2"><Button disabled={!answer.trim() || conflict} loading={save.isPending} onClick={() => save.mutate({ value: answer, version: editVersion })}>Save and confirm for future use</Button><Button variant="secondary" disabled={save.isPending} onClick={() => setEditing(false)}>Cancel</Button></div></div> : <div className="flex flex-wrap gap-2"><Button disabled={!original.trim() || conflict} loading={save.isPending} onClick={() => save.mutate({ value: original, version: { revision: item.revision, profile_revision: item.profile_revision } })}>Confirm for future use</Button><Button variant="secondary" disabled={save.isPending} onClick={() => { setEditVersion({ revision: item.revision, profile_revision: item.profile_revision }); setEditing(true) }}>{original ? 'Edit for future use' : 'Add answer'}</Button><Button variant="secondary" loading={dismiss.isPending} disabled={save.isPending} onClick={() => dismiss.mutate()}>Dismiss review</Button></div>)}
    <Link className="inline-flex min-h-11 items-center text-xs text-[var(--color-accent)] hover:underline focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]" to={'/settings/resume?view=application-answers&answers=confirmation&review=' + encodeURIComponent(item.id)}>Link to this question</Link>
    {save.isError && <p role="alert" className="text-sm text-red-400">{save.error instanceof Error ? save.error.message : 'Could not confirm this answer.'} Your draft is still here.</p>}
    {conflict && <Button variant="secondary" loading={refresh.isPending} onClick={() => refresh.mutate()}>Refresh review before retrying</Button>}
    {refresh.isError && <p role="alert" className="text-sm text-red-400">Could not refresh this review. Your draft is still here.</p>}
    {dismiss.isError && <p role="alert" className="text-sm text-red-400">Could not dismiss this review. Your answer is unchanged.</p>}
  </article>
}

export function ApplicationAnswerReviews({ items, unresolvedCount, loading, error, hasMore, loadingMore, onLoadMore, onBack }: { items: ApplicationAnswerReview[]; unresolvedCount: number; loading: boolean; error: boolean; hasMore: boolean; loadingMore: boolean; onLoadMore: () => void; onBack: () => void }) {
  return <div className="space-y-4">
    <p className="max-w-prose text-sm text-[var(--color-text-dim)]">Confirm an answer to update what the bot uses in future applications. Previous application answers stay unchanged. Reading this view does not confirm anything.</p>
    {loading && <p role="status" className="text-sm">Loading answers to confirm…</p>}
    {error && <p role="alert" className="text-sm text-red-400">Could not load answers to confirm. Try again.</p>}
    {!loading && !error && unresolvedCount === 0 && <div className="space-y-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] px-5 py-8 text-center"><Check size={24} aria-hidden className="mx-auto text-[var(--color-accent)]" /><h3 className="font-semibold">You’re all caught up</h3><p className="text-sm text-[var(--color-text-dim)]">New answers needing your attention will appear here.</p><Button variant="secondary" onClick={onBack}>Return to common answers</Button></div>}
    {items.map(item => <ApplicationAnswerReviewRow key={item.id} item={item} />)}
    {hasMore && <Button variant="secondary" loading={loadingMore} onClick={onLoadMore}>Load more answers</Button>}
  </div>
}
