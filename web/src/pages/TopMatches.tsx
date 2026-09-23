import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { TrendingUp } from 'lucide-react'
import { TopMatchJobItem } from '../components/jobs/top-match-job-item'
import {
  InfoBanner,
  JobListEmpty,
  JobListPage,
  JobListPanel,
  JobListSkeleton,
  JobSearchBar,
  ListFooter,
  LoadMoreButton,
} from '../components/jobs/job-list-shared'
import { jobsApi } from '../api/jobs'
import { settingsApi } from '../api/settings'
import type { PendingReview } from '../types'

const PAGE_SIZE = 50

export function TopMatches() {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')

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

  const all = useMemo(() => data?.pages.flat() ?? [], [data?.pages])

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deletePendingReview,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-top-matches'] }),
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
    },
  })
  const blacklistAndClearMutation = useMutation({
    mutationFn: async (job: PendingReview) => {
      await jobsApi.blacklistCompany(job.job_id)
      const others = all.filter(j => j.company.toLowerCase() === job.company.toLowerCase() && j.job_id !== job.job_id)
      await Promise.all(others.map(j => jobsApi.deletePendingReview(j.job_id)))
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-top-matches'] })
      qc.invalidateQueries({ queryKey: ['settings-preferences'] })
    },
  })

  const filtered = useMemo(
    () =>
      search
        ? all.filter(
            j =>
              j.company.toLowerCase().includes(search.toLowerCase()) ||
              j.role.toLowerCase().includes(search.toLowerCase()),
          )
        : all,
    [all, search],
  )

  const pending = deleteMutation.isPending || applyMutation.isPending || blacklistMutation.isPending || blacklistAndClearMutation.isPending

  return (
    <JobListPage
      title="Top Matches"
      description="Strong matches that do not support automated application."
    >
      <InfoBanner variant="accent">
        <TrendingUp size={18} className="shrink-0 text-[var(--color-accent)] mt-0.5" />
        <span>Roles at or above your suitability threshold. Apply manually or mark applied when done.</span>
      </InfoBanner>

      <JobSearchBar value={search} onChange={setSearch} />

      <JobListPanel>
        {isLoading && <JobListSkeleton />}
        {!isLoading && filtered.length === 0 && (
          <JobListEmpty
            message="No strong manual matches yet. Jobifai will add high-scoring roles here when automated application isn't available."
          />
        )}
        {filtered.map(job => (
          <TopMatchJobItem
            key={job.job_id}
            job={job}
            halalEnabled={halalEnabled}
            pending={pending}
            onMarkApplied={id => applyMutation.mutate(id)}
            onDelete={id => deleteMutation.mutate(id)}
            onBlacklist={id => blacklistMutation.mutate(id)}
            onBlacklistAndClear={j => blacklistAndClearMutation.mutate(j)}
          />
        ))}
      </JobListPanel>

      {hasNextPage && <LoadMoreButton loading={isFetchingNextPage} onClick={() => fetchNextPage()} />}
      <ListFooter shown={filtered.length} />
    </JobListPage>
  )
}
