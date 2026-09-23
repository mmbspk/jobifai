import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { SkippedJobItem } from '../components/jobs/skipped-job-item'
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
import { settingsApi } from '../api/settings'

const PAGE_SIZE = 50

const SKIP_REASONS = [
  '', 'Already applied', 'Blacklisted company', 'Blacklisted title',
  'Blacklisted location', 'Company re-apply limit', 'Below suitability threshold', 'Manual skip',
]

const PLATFORMS = [
  { value: '', label: 'All platforms' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
]

export function JobsSkipped() {
  const qc = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const todayFilter = searchParams.get('today') === 'true'
  const [search, setSearch] = useState('')
  const [platform, setPlatform] = useState('')
  const [skipReason, setSkipReason] = useState('')

  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-skipped', platform, skipReason, todayFilter],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.skipped({
        platform: platform || undefined,
        skip_reason: skipReason || undefined,
        today: todayFilter || undefined,
        limit: PAGE_SIZE,
        offset: pageParam,
      }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
  })

  const deleteMutation = useMutation({
    mutationFn: jobsApi.deleteSkipped,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-skipped'] }),
  })

  const all = data?.pages.flat() ?? []
  const filtered = search
    ? all.filter(j => j.company.toLowerCase().includes(search.toLowerCase()) || j.role.toLowerCase().includes(search.toLowerCase()))
    : all

  return (
    <JobListPage title="Skipped" description="Roles Jobifai viewed but did not apply to, with reasons.">
      <JobSearchBar value={search} onChange={setSearch}>
        <FilterSelect value={platform} onChange={setPlatform} aria-label="Platform">
          {PLATFORMS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
        </FilterSelect>
        <FilterSelect value={skipReason} onChange={setSkipReason} aria-label="Skip reason">
          {SKIP_REASONS.map(r => <option key={r} value={r}>{r || 'All reasons'}</option>)}
        </FilterSelect>
      </JobSearchBar>

      {todayFilter && <ActiveFilterChip label="Today only" onClear={() => setSearchParams({})} />}

      <JobListPanel>
        {isLoading && <JobListSkeleton />}
        {!isLoading && filtered.length === 0 && (
          <JobListEmpty message="No skipped roles in this view." />
        )}
        {filtered.map(job => (
          <SkippedJobItem
            key={job.id}
            job={job}
            halalEnabled={halalEnabled}
            onDelete={id => deleteMutation.mutate(id)}
            deletePending={deleteMutation.isPending}
          />
        ))}
      </JobListPanel>

      {hasNextPage && <LoadMoreButton loading={isFetchingNextPage} onClick={() => fetchNextPage()} />}
      <ListFooter shown={filtered.length} />
    </JobListPage>
  )
}
