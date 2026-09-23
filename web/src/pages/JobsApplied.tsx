import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AppliedJobItem } from '../components/jobs/applied-job-item'
import {
  ActiveFilterChip,
  FilterSelect,
  JobListEmpty,
  JobListPage,
  JobListPanel,
  JobListSkeleton,
  JobSearchBar,
  ListFooter,
  LoadMoreButton,
} from '../components/jobs/job-list-shared'
import { jobsApi } from '../api/jobs'

const PAGE_SIZE = 50

const PLATFORMS: { value: string; label: string }[] = [
  { value: '', label: 'All platforms' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
]

export function JobsApplied() {
  const qc = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const todayFilter = searchParams.get('today') === 'true'
  const [search, setSearch] = useState('')
  const [platform, setPlatform] = useState('')

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-applied', platform, todayFilter],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.applied({ platform: platform || undefined, today: todayFilter || undefined, limit: PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
    staleTime: 60_000,
  })

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deleteApplied,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-applied'] }),
  })

  const all = data?.pages.flat() ?? []
  const filtered = search
    ? all.filter(j => j.company.toLowerCase().includes(search.toLowerCase()) || j.role.toLowerCase().includes(search.toLowerCase()))
    : all

  return (
    <JobListPage
      title="Applied"
      description="Applications submitted by Jobifai."
    >
      <JobSearchBar value={search} onChange={setSearch}>
        <FilterSelect value={platform} onChange={setPlatform} aria-label="Platform filter">
          {PLATFORMS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
        </FilterSelect>
      </JobSearchBar>

      {todayFilter && (
        <ActiveFilterChip label="Today only" onClear={() => setSearchParams({})} />
      )}

      <JobListPanel>
        {isLoading && <JobListSkeleton />}
        {!isLoading && filtered.length === 0 && (
          <JobListEmpty
            message="No applications yet. Start automation or prepare an application manually."
            action={{ label: 'Go to dashboard', to: '/' }}
          />
        )}
        {filtered.map(job => (
          <AppliedJobItem
            key={job.id}
            job={job}
            onDelete={id => deleteMutation.mutate(id)}
            deletePending={deleteMutation.isPending}
          />
        ))}
      </JobListPanel>

      {hasNextPage && <LoadMoreButton loading={isFetchingNextPage} onClick={() => fetchNextPage()} />}
      <ListFooter shown={filtered.length} total={all.length} />
    </JobListPage>
  )
}
