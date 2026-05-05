import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ExternalLink, ChevronDown, ChevronRight, TrendingUp, Trash2, CheckCheck, Ban, CircleX, X } from 'lucide-react'
import { ScorePill } from '../components/ScorePill'
import { PlatformBadge } from '../components/PlatformBadge'
import { cn, formatDate, relativeTime } from '../lib'
import { jobsApi } from '../api/jobs'
import { settingsApi } from '../api/settings'

const PAGE_SIZE = 50

export function TopMatches() {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [pendingBlacklist, setPendingBlacklist] = useState<string | null>(null)

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-top-matches'],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.topMatches({ limit: PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
    staleTime: 60_000,
  })
  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get, staleTime: 300_000 })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deletePendingReview,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-top-matches'] })
      setPendingDelete(null)
    },
  })

  const applyMutation = useMutation({
    mutationFn: jobsApi.markApplied,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-top-matches'] })
      qc.invalidateQueries({ queryKey: ['jobs-stats'] })
    },
  })

  const blacklistMutation = useMutation({
    mutationFn: jobsApi.blacklistCompany,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-top-matches'] })
      qc.invalidateQueries({ queryKey: ['settings-preferences'] })
      setPendingBlacklist(null)
    },
  })

  const blacklistAndClearMutation = useMutation({
    mutationFn: async (job: (typeof all)[number]) => {
      await jobsApi.blacklistCompany(job.job_id)
      const others = all.filter(j => j.company.toLowerCase() === job.company.toLowerCase() && j.job_id !== job.job_id)
      await Promise.all(others.map(j => jobsApi.deletePendingReview(j.job_id)))
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-top-matches'] })
      qc.invalidateQueries({ queryKey: ['settings-preferences'] })
      setPendingBlacklist(null)
    },
  })

  const all = data?.pages.flat() ?? []
  const filtered = search
    ? all.filter(j =>
        j.company.toLowerCase().includes(search.toLowerCase()) ||
        j.role.toLowerCase().includes(search.toLowerCase()),
      )
    : all

  function toggleExpand(id: string) {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-3 p-3 rounded-lg bg-violet-500/10 border border-violet-500/20 text-violet-300 text-sm">
        <TrendingUp size={16} className="shrink-0 mt-0.5" />
        <span>
          Jobs that scored at or above your suitability threshold, your best matches waiting for review.
        </span>
      </div>

      <div className="flex flex-col sm:flex-row gap-2">
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Search company or role…"
          className="flex-1 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg px-3 py-2 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50"
        />
      </div>

      <div className="rounded-xl border border-[var(--color-border)] overflow-x-auto">
        <table className="w-full table-fixed min-w-[860px] text-sm">
          <colgroup>
            <col className="w-6" />
            <col className="w-[78px]" />
            <col className="w-[130px]" />
            <col className="w-[150px]" />
            <col className="w-[110px]" />
            <col className="w-[88px]" />
            <col className="w-[68px]" />
            <col className="w-[58px]" />
            <col className="w-[38px]" />
            <col className="w-[50px]" />
            <col className="w-[84px]" />
          </colgroup>
          <thead className="hidden sm:table-header-group">
            <tr className="border-b border-[var(--color-border)] bg-[var(--color-surface)]">
              <th className="w-6 text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)]"></th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Posted</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Company</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)]">Role</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Location</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Platform</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Due</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Score</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Link</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Applied</th>
              <th className="text-left px-2 py-2 text-xs font-normal text-[var(--color-text-dim)]"></th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr><td colSpan={11} className="px-4 py-8 text-center text-sm text-[var(--color-text-dim)]">Loading…</td></tr>
            )}
            {!isLoading && filtered.length === 0 && (
              <tr>
                <td colSpan={11} className="px-4 py-12 text-center text-sm text-[var(--color-text-dim)]">
                  No top matches yet, jobs scored at or above your suitability threshold will appear here.
                </td>
              </tr>
            )}
            {filtered.map(job => {
              const isExpanded = expanded.has(job.job_id)
              const isDoubtful = halalEnabled && job.halal_verdict?.verdict === 'DOUBTFUL'
              const isExpandable = !!(job.suitability_reasoning || isDoubtful)
              return (
                <>
                  <tr
                    key={job.job_id}
                    className={cn(
                      'border-b border-[var(--color-border-subtle)] last:border-0 hover:bg-[var(--color-surface-2)] transition-colors',
                      isExpandable && 'cursor-pointer',
                      isExpanded && 'bg-[var(--color-surface-2)]',
                      isDoubtful && !isExpanded && 'bg-orange-500/10',
                    )}
                    onClick={() => isExpandable && toggleExpand(job.job_id)}
                  >
                    <td className="px-2 py-2 align-top text-center">
                      {job.easy_apply && (
                        <span title="Easy Apply / Quick Apply" className="text-amber-400 text-sm">⚡</span>
                      )}
                    </td>
                    <td className="px-2 py-2 align-top text-xs text-[var(--color-text-dim)] whitespace-nowrap">
                      <span title={formatDate(job.posted_date || job.created_at)} className="cursor-default">
                        {relativeTime(job.posted_date || job.created_at)}
                      </span>
                    </td>
                    <td className="px-2 py-2 align-top font-medium text-[var(--color-text)] max-w-[140px]">
                      <span className="flex items-center gap-1" title={job.company}>
                        {isExpandable && (
                          <ChevronRight size={12} className={cn('text-[var(--color-text-dim)] transition-transform shrink-0', isExpanded && 'rotate-90')} />
                        )}
                        <span className="truncate">{job.company || '—'}</span>
                      </span>
                    </td>
                    <td className="px-2 py-2 align-top max-w-[160px]">
                      <div className="line-clamp-2 text-[var(--color-text-muted)] leading-snug text-sm" title={job.role}>{job.role}</div>
                    </td>
                    <td className="px-2 py-2 align-top text-xs text-[var(--color-text-dim)] max-w-[110px]">
                      <div className="truncate" title={job.location || undefined}>{job.location || '—'}</div>
                    </td>
                    <td className="px-2 py-2 align-top whitespace-nowrap"><PlatformBadge platform={job.platform} size="sm" /></td>
                    <td className="px-2 py-2 align-top text-xs text-[var(--color-text-dim)] whitespace-nowrap">
                      {job.due_date || '—'}
                    </td>
                    <td className="px-2 py-2 align-top whitespace-nowrap">
                      {job.suitability_score != null && job.suitability_score > 0
                        ? <ScorePill score={job.suitability_score} />
                        : <span className="text-[var(--color-text-dim)]">—</span>
                      }
                    </td>
                    <td className="px-2 py-2 align-top whitespace-nowrap">
                      {job.link ? (
                        <a
                          href={job.link}
                          target="_blank"
                          rel="noopener noreferrer"
                          title="Open job listing"
                          className="text-amber-400/70 hover:text-amber-300"
                          onClick={e => e.stopPropagation()}
                        >
                          <ExternalLink size={12} />
                        </a>
                      ) : (
                        <span className="text-[var(--color-text-dim)]">—</span>
                      )}
                    </td>
                    <td className="px-2 py-2 align-top whitespace-nowrap">
                      <button
                        onClick={e => { e.stopPropagation(); applyMutation.mutate(job.job_id) }}
                        disabled={applyMutation.isPending}
                        title="Mark as Applied"
                        className="text-[var(--color-text-dim)] hover:text-emerald-400 disabled:opacity-40 transition-colors"
                      >
                        <CheckCheck size={13} />
                      </button>
                    </td>
                    <td className="px-2 py-2 align-top whitespace-nowrap">
                      <div className="flex items-center gap-2">
                        {pendingBlacklist === job.job_id ? (
                          <span className="flex items-center gap-1.5">
                            <button title="Blacklist company" onClick={e => { e.stopPropagation(); blacklistMutation.mutate(job.job_id) }} disabled={blacklistMutation.isPending || blacklistAndClearMutation.isPending} className="text-orange-400 hover:text-orange-300 disabled:opacity-40 transition-colors"><Ban size={13} /></button>
                            <button title="Blacklist + remove all from Top Matches" onClick={e => { e.stopPropagation(); blacklistAndClearMutation.mutate(job) }} disabled={blacklistMutation.isPending || blacklistAndClearMutation.isPending} className="text-red-400 hover:text-red-300 disabled:opacity-40 transition-colors"><CircleX size={13} /></button>
                            <button title="Cancel" onClick={e => { e.stopPropagation(); setPendingBlacklist(null) }} className="text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors"><X size={13} /></button>
                          </span>
                        ) : (
                          <button title="Blacklist Company" onClick={e => { e.stopPropagation(); setPendingDelete(null); setPendingBlacklist(job.job_id) }} className="text-[var(--color-text-dim)] hover:text-orange-400"><Ban size={13} /></button>
                        )}
                        <span className="text-[var(--color-border)] select-none">|</span>
                        {pendingDelete === job.job_id ? (
                          <span className="flex items-center gap-1.5">
                            <button title="Confirm delete" onClick={e => { e.stopPropagation(); deleteMutation.mutate(job.job_id) }} disabled={deleteMutation.isPending} className="text-red-400 hover:text-red-300 disabled:opacity-40 transition-colors"><Trash2 size={13} /></button>
                            <button title="Cancel" onClick={e => { e.stopPropagation(); setPendingDelete(null) }} className="text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors"><X size={13} /></button>
                          </span>
                        ) : (
                          <button title="Delete" onClick={e => { e.stopPropagation(); setPendingBlacklist(null); setPendingDelete(job.job_id) }} className="text-[var(--color-text-dim)] hover:text-red-400"><Trash2 size={13} /></button>
                        )}
                      </div>
                    </td>
                  </tr>
                  {isExpanded && isExpandable && (
                    <tr key={`${job.job_id}-expand`} className={cn('border-b border-[var(--color-border-subtle)]', isDoubtful ? 'bg-orange-500/8' : 'bg-[var(--color-surface)]')}>
                      <td colSpan={11} className="px-8 py-3 space-y-2">
                        {job.suitability_reasoning && (
                          <p className="text-xs text-[var(--color-text-muted)] leading-relaxed">{job.suitability_reasoning}</p>
                        )}
                        {isDoubtful && job.halal_verdict && (
                          <div className={cn('space-y-1', job.suitability_reasoning && 'border-t border-orange-500/20 pt-2')}>
                            <p className="text-xs font-medium text-orange-300/80">
                              Islamic ethics: DOUBTFUL ({job.halal_verdict.confidence} confidence)
                            </p>
                            <p className="text-xs text-[var(--color-text-muted)]">{job.halal_verdict.summary}</p>
                            {job.halal_verdict.reasons.map(r => (
                              <p key={r} className="text-xs text-[var(--color-text-dim)]">· {r}</p>
                            ))}
                          </div>
                        )}
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>

      {hasNextPage && (
        <div className="flex justify-center">
          <button
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="px-4 py-2 text-sm text-[var(--color-text-muted)] border border-[var(--color-border)] rounded-lg hover:bg-[var(--color-surface-2)] disabled:opacity-50 flex items-center gap-1"
          >
            <ChevronDown size={14} />
            {isFetchingNextPage ? 'Loading…' : 'Load more'}
          </button>
        </div>
      )}

      <div className="text-xs text-[var(--color-text-dim)] text-right">
        {filtered.length} shown
      </div>
    </div>
  )
}
