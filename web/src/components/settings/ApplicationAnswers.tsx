import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import * as Tabs from '@radix-ui/react-tabs'
import { Bell, Check } from 'lucide-react'
import { settingsApi, type ApplicationAnswersResponse, type ApplicationAnswerSource, type CVAnswerPrefill } from '../../api/settings'
import type { ApplicationDefaults, Certification } from '../../types'
import { validateApplicationDefaults } from '../../lib/applicationDefaults'
import { Button } from '../Button'
import { CommonApplicationAnswers } from './CommonApplicationAnswers'
import { applicationAnswerTopicLabels, applicationTopicValues } from '../../lib/applicationAnswerTopics'
import { ApplicationAnswerReviews, ApplicationAnswerReviewRow } from './ApplicationAnswerReviews'
import { useApplicationAnswerReviews } from '../../hooks/useApplicationAnswerReviews'

const fields: Record<string, (keyof ApplicationDefaults)[]> = {
  work_rights: ['work_rights'], sponsorship: ['requires_sponsorship', 'future_requires_sponsorship'],
  notice_period: ['notice_period', 'availability'], earliest_start_date: ['earliest_start_date'],
  salary: ['salary_expectation', 'salary_range'], relocation: ['relocation'], travel: ['travel_conditions'],
  working_hours: ['working_hours'], work_arrangement: ['commute_conditions'], additional_answers: ['custom_answers', 'additional_details'],
}
function retainEdited(saved: ApplicationDefaults, draft: ApplicationDefaults, topics: Iterable<string>) {
  const next = { ...saved }
  for (const topic of topics) for (const key of fields[topic] ?? []) Object.assign(next, { [key]: draft[key] })
  return next
}

export interface ApplicationProfileDraft {
  value: ApplicationDefaults
  prefill: CVAnswerPrefill[]
  certifications?: Certification[]
  onSave: () => Promise<unknown>
  saving: boolean
  disabled: boolean
  error?: string
}

function importedSources(draft: ApplicationProfileDraft, saved?: ApplicationDefaults): Record<string, ApplicationAnswerSource> {
  const sources: Record<string, ApplicationAnswerSource> = {}
  for (const item of draft.prefill) {
    if (item.needs_confirmation || item.source !== 'cv_evidence' || !item.binding.startsWith('application_defaults.')) continue
    const key = item.binding.slice('application_defaults.'.length) as keyof ApplicationDefaults
    const current = draft.value[key]
    const matched = key === 'work_rights' && Array.isArray(item.value) && Array.isArray(current)
      ? item.value.every(imported => current.some(value => JSON.stringify(value) === JSON.stringify(imported)))
      : JSON.stringify(current) === JSON.stringify(item.value)
    if (!matched || JSON.stringify(current) === JSON.stringify(saved?.[key])) continue
    sources[item.topic_id] = { source: 'cv', label: saved?.[key] ? 'From profile and CV' : 'From CV — review before saving', evidence: item.evidence.map(e => e.quote) }
  }
  return sources
}

export function ApplicationAnswers({ onEditProfile, initialView = 'common', reviewId, profileDraft, onDraftChange }: { onEditProfile: () => void; initialView?: string; reviewId?: string; profileDraft?: ApplicationProfileDraft; onDraftChange?: (value: ApplicationDefaults) => void }) {
  const qc = useQueryClient()
  const query = useQuery({ queryKey: ['application-answers'], queryFn: settingsApi.applicationAnswers.get })
  const reviews = useApplicationAnswerReviews()
  const [historyOpen, setHistoryOpen] = useState(false)
  const history = useApplicationAnswerReviews(historyOpen, 'history')
  const detail = useQuery({ queryKey: ['application-answer-review', reviewId], queryFn: () => settingsApi.applicationAnswers.review(reviewId!), enabled: !!reviewId })
  const [view, setView] = useState(initialView === 'confirmation' ? 'confirmation' : 'common')
  const [draft, setDraft] = useState<ApplicationDefaults>({})
  const [revision, setRevision] = useState('')
  const [saved, setSaved] = useState(false)
  const [dirtyCount, setDirtyCount] = useState(0)
  const [editedTopics, setEditedTopics] = useState<string[]>([])
  const [remoteChanged, setRemoteChanged] = useState(false)
  const dirty = useRef(new Map<string, number>())
  const generation = useRef(0)
  const initialized = useRef(false)
  const [openTopic, setOpenTopic] = useState<{ id: string; request: number }>()
  const visibleDraft = profileDraft?.value ?? draft
  const errors = validateApplicationDefaults(visibleDraft)
  useEffect(() => {
    if (!query.data) return
    if (!initialized.current || dirty.current.size === 0) {
      initialized.current = true
      setDraft(query.data.application_defaults)
      setRevision(query.data.revision)
      setRemoteChanged(false)
    } else if (query.data.revision !== revision) setRemoteChanged(true)
  }, [query.data, revision])
  const save = useMutation({
    mutationFn: ({ topics, version }: { topics: Record<string, unknown>; version: number }) => settingsApi.applicationAnswers.set({ topics, revision }).then(response => ({ response, version })),
    onSuccess: ({ response, version }) => {
      for (const [topic, editedAt] of dirty.current) if (editedAt <= version) dirty.current.delete(topic)
      setDirtyCount(dirty.current.size)
      setEditedTopics([...dirty.current.keys()])
      setDraft(current => retainEdited(response.application_defaults, current, dirty.current.keys()))
      setRevision(response.revision)
      setRemoteChanged(false)
      qc.setQueryData(['application-answers'], response)
      setSaved(true)
      void qc.invalidateQueries({ queryKey: ['application-answer-reviews'] })
    },
  })
  const reload = useMutation({
    mutationFn: settingsApi.applicationAnswers.get,
    onSuccess: (response: ApplicationAnswersResponse) => {
      setDraft(current => retainEdited(response.application_defaults, current, dirty.current.keys()))
      setRevision(response.revision)
      setRemoteChanged(false)
      qc.setQueryData(['application-answers'], response)
      save.reset()
    },
  })
  const saveProfileDraft = useMutation({
    mutationFn: async () => {
      if (!profileDraft) throw new Error('No profile draft is available')
      const version = generation.current
      await profileDraft.onSave()
      return version
    },
    onSuccess: version => {
      for (const [topic, editedAt] of dirty.current) if (editedAt <= version) dirty.current.delete(topic)
      setDirtyCount(dirty.current.size)
      setEditedTopics([...dirty.current.keys()])
      setSaved(true)
      void qc.invalidateQueries({ queryKey: ['application-answers'] })
    },
  })
  const read = useMutation({ mutationFn: settingsApi.applicationAnswers.markRead, onSuccess: () => { void qc.invalidateQueries({ queryKey: ['application-answer-reviews'] }) } })
  const counts = reviews.data?.pages[0]
  function openView(next: string) { setView(next); if (next === 'confirmation' && (counts?.unread_count ?? 0) > 0) read.mutate() }
  useEffect(() => { if (initialView === 'confirmation') setView('confirmation') }, [initialView])
  const items = reviews.data?.pages.flatMap(page => page.items).filter(item => item.id !== detail.data?.id) ?? []
  const catalog = (query.data?.catalog?.length ? query.data.catalog : Object.entries(applicationAnswerTopicLabels).filter(([id]) => id !== 'additional_answers').map(([id, label]) => ({ id, label, core: ['work_rights', 'sponsorship', 'notice_period', 'earliest_start_date', 'salary'].includes(id), summary: '' }))).map(topic => ({ ...topic, summary: topic.summary || (topic.id === 'credentials' ? (profileDraft?.certifications ?? query.data?.profile?.certifications ?? query.data?.certifications)?.map(cert => cert.name).filter(Boolean).join(', ') : topic.id === 'work_arrangement' ? [['remote', 'Remote'], ['hybrid', 'Hybrid'], ['onsite', 'On site']].filter(([key]) => query.data?.preferences?.[key as 'remote' | 'hybrid' | 'onsite']).map(([, label]) => label).join(', ') : '') }))
  const sources = { ...query.data?.sources, ...(profileDraft ? importedSources(profileDraft, query.data?.application_defaults) : {}), ...Object.fromEntries(editedTopics.map(topic => [topic, { source: 'candidate' as const, label: 'Unsaved' }])) }
  function changeDraft(value: ApplicationDefaults) { setDraft(value); onDraftChange?.(value) }
  const suggestions = [...new Set(profileDraft?.prefill.map(item => item.topic_id))].map(topic => ({ topic, items: profileDraft!.prefill.filter(item => item.topic_id === topic) }))
  return <div className="space-y-5">
    <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-lg font-semibold">Application answers</h2><p className="mt-1 max-w-prose text-sm text-[var(--color-text-dim)]">Prepare answers once. Clear profile facts help you get started; unknown facts stay unspecified.</p></div>{(counts?.unread_count ?? 0) > 0 && <button type="button" onClick={() => openView('confirmation')} className="flex min-h-11 items-center gap-2 rounded-lg border border-[var(--color-border)] px-3 text-sm focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"><Bell size={16} aria-hidden />{counts?.unread_count} new {counts?.unread_count === 1 ? 'answer' : 'answers'}</button>}</div>
    <Tabs.Root value={view} onValueChange={openView}>
      <Tabs.List aria-label="Application answers views" className="mb-5 flex w-fit max-w-full flex-wrap gap-1 rounded-lg bg-[var(--color-surface-2)] p-1">{[{ id: 'common', label: 'Common answers' }, { id: 'confirmation', label: 'Needs confirmation' }].map(tab => <Tabs.Trigger key={tab.id} value={tab.id} className="flex min-h-11 items-center gap-2 rounded-md px-3 text-sm text-[var(--color-text-dim)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] data-[state=active]:bg-[var(--color-surface)] data-[state=active]:text-[var(--color-text)]">{tab.label}{tab.id === 'confirmation' && <span className="rounded-full bg-[var(--color-accent-soft)] px-1.5 text-xs text-[var(--color-accent)]">{counts?.unresolved_count ?? 0}</span>}</Tabs.Trigger>)}</Tabs.List>
      <Tabs.Content value="common" forceMount hidden={view !== 'common'} className="space-y-4 outline-none">
        {query.isLoading && !profileDraft && <p role="status" className="text-sm">Loading common answers…</p>}
        {query.isError && <div role="alert" className="space-y-2 text-sm text-red-400"><p>Could not load common answers.</p><Button variant="secondary" onClick={() => { void query.refetch() }}>Try again</Button></div>}
        {(profileDraft || revision) && <>
          {profileDraft && <section aria-label="Imported profile review" className="space-y-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4">
            <p className="text-sm">Review your profile and application answers together. Clear CV statements fill the draft; missing or uncertain facts stay unspecified.</p>
            {suggestions.length > 0 && <details className="text-sm"><summary className="min-h-11 cursor-pointer text-[var(--color-accent)] focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]">View CV answer evidence</summary><div className="space-y-4 pt-2">{suggestions.map(({ topic, items }) => <div key={topic} className="space-y-2 border-t border-[var(--color-border)] pt-3">
              <p className="font-medium">{applicationAnswerTopicLabels[topic] ?? 'Additional answer'}</p>
              <p className="text-xs text-[var(--color-text-dim)]">{items.some(item => item.needs_confirmation) ? 'Needs your clarification before use' : 'Review this fact from your CV'}</p>
              {items.flatMap(item => item.warning ? [item.warning] : []).map((warning, i) => <p key={i} className="text-xs text-[var(--color-text-dim)]">{warning}</p>)}
              {[...new Set(items.flatMap(item => item.evidence.map(e => e.quote)))].map(quote => <blockquote key={quote} className="break-words text-xs text-[var(--color-text-dim)]">“{quote}”</blockquote>)}
              <Button variant="secondary" onClick={() => setOpenTopic(current => ({ id: topic, request: (current?.request ?? 0) + 1 }))}>Review {applicationAnswerTopicLabels[topic]?.toLowerCase() ?? 'additional answer'}</Button>
            </div>)}</div></details>}
          </section>}
          <CommonApplicationAnswers value={visibleDraft} onChange={changeDraft} onTopicChange={topic => { dirty.current.set(topic, ++generation.current); setDirtyCount(dirty.current.size); setEditedTopics([...dirty.current.keys()]); setSaved(false) }} sources={sources} catalog={catalog} onEditProfile={onEditProfile} openTopic={openTopic} />
          {errors.length > 0 && <div role="alert" className="text-sm text-red-400"><ul className="list-disc space-y-1 pl-4">{errors.map(error => <li key={error}>{error}</li>)}</ul></div>}
          {!profileDraft && (remoteChanged || save.isError) && <div className="space-y-2"><p role="alert" className="text-sm text-red-400">{save.isError ? save.error instanceof Error ? save.error.message : 'Could not save answers.' : 'Saved answers changed while you were editing.'} Your edits are still here.</p><Button variant="secondary" loading={reload.isPending} onClick={() => reload.mutate()}>Load latest answers and keep my edits</Button></div>}
          {reload.isError && <p role="alert" className="text-sm text-red-400">Could not load the latest answers. Your edits are still here.</p>}
          {profileDraft && (profileDraft.error || saveProfileDraft.isError) && <div className="space-y-2"><p role="alert" className="text-sm text-red-400">{profileDraft.error ?? (saveProfileDraft.error instanceof Error ? saveProfileDraft.error.message : 'Could not save the reviewed profile.')} Your draft is still here.</p><Button variant="secondary" onClick={onEditProfile}>Review profile and resolve changes</Button></div>}
          <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-xs text-[var(--color-text-dim)]">All answers are optional. Save only facts that are accurate for you.</p>{profileDraft ? <Button loading={profileDraft.saving || saveProfileDraft.isPending} disabled={errors.length > 0 || profileDraft.disabled} onClick={() => saveProfileDraft.mutate()}>Save profile and answers</Button> : <Button loading={save.isPending} disabled={errors.length > 0 || !revision || dirtyCount === 0 || remoteChanged} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => { const values = applicationTopicValues(draft); const topics = Object.fromEntries([...dirty.current.keys()].map(topic => [topic, values[topic]])); save.mutate({ topics, version: generation.current }) }}>{saved ? 'Saved' : 'Save common answers'}</Button>}</div>
          {saved && <p role="status" className="text-sm text-[var(--color-accent)]">{saveProfileDraft.isSuccess ? 'Profile and application answers saved.' : 'Common answers saved.'}</p>}
        </>}
      </Tabs.Content>
      <Tabs.Content value="confirmation" forceMount hidden={view !== 'confirmation'} className="space-y-4 outline-none">
        {reviewId && detail.isLoading && <p role="status" className="text-sm">Loading this question…</p>}
        {reviewId && detail.isError && <div role="alert" className="space-y-2 text-sm text-red-400"><p>Could not load this question. It may no longer be available.</p><Button variant="secondary" onClick={() => { void detail.refetch() }}>Try loading this question again</Button></div>}
        {detail.data && <ApplicationAnswerReviewRow item={detail.data} />}
        <ApplicationAnswerReviews items={items} unresolvedCount={counts?.unresolved_count ?? 0} loading={reviews.isLoading} error={reviews.isError} hasMore={reviews.hasNextPage} loadingMore={reviews.isFetchingNextPage} onLoadMore={() => { void reviews.fetchNextPage() }} onBack={() => setView('common')} />
        {reviews.isError && <Button variant="secondary" onClick={() => { void reviews.refetch() }}>Try loading reviews again</Button>}
        {read.isError && <p role="alert" className="text-sm text-red-400">Could not mark notifications read. Your confirmation decisions are unchanged.</p>}
        <section className="border-t border-[var(--color-border)] pt-3"><button type="button" aria-expanded={historyOpen} aria-controls="answer-history" onClick={() => setHistoryOpen(current => !current)} className="min-h-11 text-sm text-[var(--color-accent)] focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]">{historyOpen ? 'Hide previous decisions' : 'Show previous decisions'}</button><div id="answer-history" hidden={!historyOpen} className="space-y-3 pt-3">
          {history.isLoading && <p role="status" className="text-sm">Loading previous decisions…</p>}
          {history.isError && <p role="alert" className="text-sm text-red-400">Could not load previous decisions.</p>}
          {history.data?.pages.flatMap(page => page.items).filter(item => item.id !== detail.data?.id).map(item => <ApplicationAnswerReviewRow key={item.id} item={item} />)}
          {!history.isLoading && !history.isError && history.data?.pages[0].items.length === 0 && <p className="text-sm text-[var(--color-text-dim)]">No previous decisions yet.</p>}
          {history.hasNextPage && <Button variant="secondary" loading={history.isFetchingNextPage} onClick={() => { void history.fetchNextPage() }}>Load more previous decisions</Button>}
        </div></section>
      </Tabs.Content>
    </Tabs.Root>
  </div>
}
