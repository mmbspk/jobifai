import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ExternalLink, ChevronDown, ChevronRight, AlertCircle, RotateCcw, Trash2 } from 'lucide-react'
import { PlatformBadge } from '../components/PlatformBadge'
import { ScorePill } from '../components/ScorePill'
import { cn, formatDate, relativeTime } from '../lib'
import { jobsApi } from '../api/jobs'
import { settingsApi } from '../api/settings'

const PAGE_SIZE = 50

const PLATFORMS = [
  { value: '', label: 'All platforms' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
  { value: 'indeed', label: 'Indeed' },
]



export function JobsCannotApply() {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [platform, setPlatform] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-cannot-apply', platform],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.cannotApply({
        platform: platform || undefined,
        limit: PAGE_SIZE,
        offset: pageParam,
      }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
  })

  const requeue = useMutation({
    mutationFn: jobsApi.requeueCannotApply,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] }),
  })

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deleteSkipped,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] })
      setPendingDelete(null)
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
      <div className="flex items-start gap-3 p-3 rounded-lg bg-amber-500/10 border border-amber-500/20 text-amber-300 text-sm">
        <AlertCircle size={16} className="shrink-0 mt-0.5" />
        <span>
          These jobs were found and scored, but jobifai couldn't submit the application automatically
          (e.g. external ATS redirect, Easy Apply not available). You can apply to them manually.
        </span>
      </div>

      <div className="flex flex-col sm:flex-row gap-2">
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Search company or role…"
          className="flex-1 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg px-3 py-2 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50"
        />
        <select
          value={platform}
          onChange={e => setPlatform(e.target.value)}
          className="bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg px-3 py-2 text-sm text-[var(--color-text)] outline-none focus:border-violet-500/50"
        >
          {PLATFORMS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
        </select>
      </div>

      <div className="rounded-xl border border-[var(--color-border)] overflow-hidden">
        <table className="w-full table-auto text-sm">
          <thead className="hidden sm:table-header-group">
            <tr className="border-b border-[var(--color-border)] bg-[var(--color-surface)]">
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Company</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)]">Role</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Platform</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Score</th>
              {halalEnabled && <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Halal</th>}
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Date</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)]"></th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr><td colSpan={halalEnabled ? 7 : 6} className="px-4 py-8 text-center text-sm text-[var(--color-text-dim)]">Loading…</td></tr>
            )}
            {!isLoading && filtered.length === 0 && (
              <tr><td colSpan={halalEnabled ? 7 : 6} className="px-4 py-12 text-center text-sm text-[var(--color-text-dim)]">No jobs here</td></tr>
            )}
            {filtered.map(job => {
              const isExpanded = expanded.has(job.id)
              return (
                <>
                  <tr
                    key={job.id}
                    className={cn(
                      'border-b border-[var(--color-border-subtle)] last:border-0 hover:bg-[var(--color-surface-2)] transition-colors',
                      (job.suitability_reasoning || (halalEnabled && job.halal_verdict)) && 'cursor-pointer',
                      isExpanded && 'bg-[var(--color-surface-2)]',
                    )}
                    onClick={() => (job.suitability_reasoning || (halalEnabled && job.halal_verdict)) && toggleExpand(job.id)}
                  >
                    <td className="px-4 py-3 align-top font-medium text-[var(--color-text)]">
                      <span className="flex items-center gap-1" title={job.company}>
                        {(job.suitability_reasoning || (halalEnabled && job.halal_verdict)) && (
                          <ChevronRight size={12} className={cn('text-[var(--color-text-dim)] transition-transform shrink-0', isExpanded && 'rotate-90')} />
                        )}
                        <span>{job.company}</span>
                      </span>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <div className="line-clamp-2 text-[var(--color-text-muted)] leading-snug text-sm" title={job.role}>{job.role}</div>
                    </td>
                    <td className="px-4 py-3 align-top whitespace-nowrap"><PlatformBadge platform={job.platform} size="sm" /></td>
                    <td className="px-4 py-3 align-top whitespace-nowrap">
                      {job.suitability_score != null && job.suitability_score > 0
                        ? <ScorePill score={job.suitability_score} />
                        : <span className="text-[var(--color-text-dim)]">—</span>
                      }
                    </td>
                    {halalEnabled && (
                      <td className="px-4 py-3">
                        {job.halal_verdict ? (
                          <span
                            title={`${job.halal_verdict.verdict} (${job.halal_verdict.confidence} confidence)`}
                            className={cn('inline-block w-2.5 h-2.5 rounded-full',
                              job.halal_verdict.verdict === 'HALAL'    && 'bg-emerald-400',
                              job.halal_verdict.verdict === 'HARAM'    && 'bg-red-400',
                              job.halal_verdict.verdict === 'DOUBTFUL' && 'bg-amber-400',
                            )}
                          />
                        ) : <span className="text-[var(--color-text-dim)]">—</span>}
                      </td>
                    )}
                    <td className="px-4 py-3 align-top whitespace-nowrap">
                      <div className="flex items-center gap-2">
                        <span title={formatDate(job.viewed_at)} className="text-xs text-[var(--color-text-dim)] whitespace-nowrap cursor-default">{relativeTime(job.viewed_at)}</span>
                        <button
                          onClick={e => { e.stopPropagation(); requeue.mutate(job.id) }}
                          disabled={requeue.isPending}
                          title="Re-queue for review"
                          className="text-[var(--color-text-dim)] hover:text-violet-400 disabled:opacity-40"
                        >
                          <RotateCcw size={12} />
                        </button>
                        <a
                          href={job.link}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-amber-400/70 hover:text-amber-300"
                          onClick={e => e.stopPropagation()}
                        >
                          <ExternalLink size={12} />
                        </a>
                      </div>
                    </td>
                    <td className="px-4 py-3 align-top whitespace-nowrap">
                      {pendingDelete === job.id ? (
                        <span className="flex items-center gap-1">
                          <button onClick={e => { e.stopPropagation(); deleteMutation.mutate(job.id) }} disabled={deleteMutation.isPending} className="text-xs text-red-400 hover:text-red-300 disabled:opacity-40 font-medium">Delete</button>
                          <span className="text-[var(--color-text-dim)] text-xs">/</span>
                          <button onClick={e => { e.stopPropagation(); setPendingDelete(null) }} className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]">Cancel</button>
                        </span>
                      ) : (
                        <button onClick={e => { e.stopPropagation(); setPendingDelete(job.id) }} className="text-[var(--color-text-dim)] hover:text-red-400"><Trash2 size={13} /></button>
                      )}
                    </td>
                  </tr>
                  {isExpanded && (job.suitability_reasoning || (halalEnabled && job.halal_verdict)) && (
                    <tr key={`${job.id}-expand`} className="bg-[var(--color-surface)] border-b border-[var(--color-border-subtle)]">
                      <td colSpan={halalEnabled ? 7 : 6} className="px-8 py-3">
                        {job.suitability_reasoning && (
                          <p className="text-xs text-[var(--color-text-muted)] leading-relaxed">{job.suitability_reasoning}</p>
                        )}
                        {halalEnabled && job.halal_verdict && (
                          <div className={cn('space-y-1', job.suitability_reasoning && 'border-t border-[var(--color-border-subtle)] pt-2 mt-2')}>
                            <p className="text-xs font-medium text-[var(--color-text-dim)]">
                              Islamic ethics: {job.halal_verdict.verdict} ({job.halal_verdict.confidence} confidence)
                            </p>
                            <p className="text-xs text-[var(--color-text-muted)]">{job.halal_verdict.summary}</p>
                            {job.halal_verdict.reasons.map(r => (
                              <p key={r} className="text-xs text-[var(--color-text-dim)]">· {r}</p>
                            ))}
                            {job.halal_verdict.caveats && (
                              <p className="text-xs text-[var(--color-text-dim)] italic">{job.halal_verdict.caveats}</p>
                            )}
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
