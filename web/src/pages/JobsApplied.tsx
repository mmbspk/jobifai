import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ExternalLink, ChevronDown, FileText, Trash2 } from 'lucide-react'
import { PlatformBadge } from '../components/PlatformBadge'
import { ScorePill } from '../components/ScorePill'
import { formatDate, relativeTime } from '../lib'
import { jobsApi } from '../api/jobs'

const PAGE_SIZE = 50

const PLATFORMS: { value: string; label: string }[] = [
  { value: '', label: 'All platforms' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
  { value: 'indeed', label: 'Indeed' },
]

export function JobsApplied() {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [platform, setPlatform] = useState('')
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-applied', platform],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.applied({ platform: platform || undefined, limit: PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
  })

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deleteApplied,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-applied'] })
      setPendingDelete(null)
    },
  })

  const all = data?.pages.flat() ?? []
  const filtered = search
    ? all.filter(j => j.company.toLowerCase().includes(search.toLowerCase()) || j.role.toLowerCase().includes(search.toLowerCase()))
    : all

  return (
    <div className="space-y-4">
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
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)]">Location</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Platform</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Score</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Docs</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)] whitespace-nowrap">Date</th>
              <th className="text-left px-4 py-2 text-xs font-normal text-[var(--color-text-dim)]"></th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr><td colSpan={8} className="px-4 py-8 text-center text-sm text-[var(--color-text-dim)]">Loading…</td></tr>
            )}
            {!isLoading && filtered.length === 0 && (
              <tr><td colSpan={8} className="px-4 py-12 text-center text-sm text-[var(--color-text-dim)]">No applications yet</td></tr>
            )}
            {filtered.map(job => (
              <tr
                key={job.id}
                className="border-b border-[var(--color-border-subtle)] last:border-0 hover:bg-[var(--color-surface-2)] transition-colors"
              >
                <td className="px-4 py-3 align-top font-medium text-[var(--color-text)]" title={job.company}>{job.company}</td>
                <td className="px-4 py-3 align-top">
                  <div className="line-clamp-2 text-[var(--color-text-muted)] leading-snug text-sm" title={job.role}>{job.role}</div>
                </td>
                <td className="px-4 py-3 align-top text-xs text-[var(--color-text-dim)] break-words">{job.location || '—'}</td>
                <td className="px-4 py-3 align-top whitespace-nowrap"><PlatformBadge platform={job.platform} size="sm" /></td>
                <td className="px-4 py-3 align-top whitespace-nowrap">
                  {job.suitability_score != null && job.suitability_score > 0
                    ? <ScorePill score={job.suitability_score} />
                    : <span className="text-[var(--color-text-dim)]">—</span>
                  }
                </td>
                <td className="px-4 py-3 align-top">
                  <div className="flex items-center gap-2">
                    {job.resume_path && (
                      <a href={`/api/files/${job.resume_path.replace(/^job_applications\//, '')}`} target="_blank" rel="noopener noreferrer"
                        title="Resume" className="text-[var(--color-text-dim)] hover:text-violet-400">
                        <FileText size={13} />
                      </a>
                    )}
                    {job.cover_letter_path && (
                      <a href={`/api/files/${job.cover_letter_path.replace(/^job_applications\//, '')}`} target="_blank" rel="noopener noreferrer"
                        title="Cover letter" className="text-[var(--color-text-dim)] hover:text-amber-400">
                        <FileText size={13} />
                      </a>
                    )}
                    {!job.resume_path && !job.cover_letter_path && <span className="text-[var(--color-text-dim)]">—</span>}
                  </div>
                </td>
                <td className="px-4 py-3 align-top whitespace-nowrap">
                  <div className="flex items-center gap-2">
                    <span title={formatDate(job.applied_at)} className="text-xs text-[var(--color-text-dim)] whitespace-nowrap cursor-default">{relativeTime(job.applied_at)}</span>
                    <a href={job.link} target="_blank" rel="noopener noreferrer" className="text-[var(--color-text-dim)] hover:text-violet-400">
                      <ExternalLink size={12} />
                    </a>
                  </div>
                </td>
                <td className="px-4 py-3 align-top whitespace-nowrap">
                  {pendingDelete === job.id ? (
                    <span className="flex items-center gap-1">
                      <button onClick={() => deleteMutation.mutate(job.id)} disabled={deleteMutation.isPending} className="text-xs text-red-400 hover:text-red-300 disabled:opacity-40 font-medium">Delete</button>
                      <span className="text-[var(--color-text-dim)] text-xs">/</span>
                      <button onClick={() => setPendingDelete(null)} className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]">Cancel</button>
                    </span>
                  ) : (
                    <button onClick={() => setPendingDelete(job.id)} className="text-[var(--color-text-dim)] hover:text-red-400"><Trash2 size={13} /></button>
                  )}
                </td>
              </tr>
            ))}
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
        {filtered.length} of {all.length} shown
      </div>
    </div>
  )
}
