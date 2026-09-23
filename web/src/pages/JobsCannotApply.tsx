import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { AlertCircle, Play } from 'lucide-react'
import { CannotApplyJobItem } from '../components/jobs/cannot-apply-job-item'
import {
  FilterSelect,
  InfoBanner,
  JobListEmpty,
  JobListPage,
  JobListPanel,
  JobListSkeleton,
  JobSearchBar,
  ListFooter,
  LoadMoreButton,
} from '../components/jobs/job-list-shared'
import { Button } from '../components/Button'
import { jobsApi } from '../api/jobs'
import { settingsApi } from '../api/settings'

const PAGE_SIZE = 50

const PLATFORMS = [
  { value: '', label: 'All platforms' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'seek', label: 'Seek' },
]

export function JobsCannotApply() {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [platform, setPlatform] = useState('')

  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const halalEnabled = generalSettings?.halal_job_filter === true

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['jobs-cannot-apply', platform],
    queryFn: ({ pageParam = 0 }) =>
      jobsApi.cannotApply({ platform: platform || undefined, limit: PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, all) =>
      last.length === PAGE_SIZE ? all.flat().length : undefined,
  })

  const requeue = useMutation({
    mutationFn: jobsApi.requeueCannotApply,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] }),
  })
  const retry = useMutation({
    mutationFn: jobsApi.retryCannotApply,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] }),
  })
  const retryAll = useMutation({
    mutationFn: () => jobsApi.retryAllCannotApply(platform || undefined),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] }),
  })
  const markApplied = useMutation({
    mutationFn: jobsApi.markAppliedFromCannotApply,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] })
      qc.invalidateQueries({ queryKey: ['jobs-stats'] })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: jobsApi.deleteSkipped,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs-cannot-apply'] }),
  })

  const all = data?.pages.flat() ?? []
  const filtered = search
    ? all.filter(j =>
        j.company.toLowerCase().includes(search.toLowerCase()) ||
        j.role.toLowerCase().includes(search.toLowerCase()),
      )
    : all

  const actionPending = requeue.isPending || retry.isPending || markApplied.isPending || deleteMutation.isPending

  return (
    <JobListPage
      title="Cannot Apply"
      description="These jobs need a step Jobifai cannot complete automatically."
    >
      <InfoBanner variant="warn">
        <AlertCircle size={18} className="shrink-0 text-[var(--color-warn)] mt-0.5" />
        <span>
          Easy Apply was available, but submission could not be finished (for example screening questions or validation).
          Retry queues the job for the next automation run, or continue manually on the listing.
        </span>
      </InfoBanner>

      <div className="flex flex-col sm:flex-row gap-2 sm:items-start">
        <Button
          variant="secondary"
          size="sm"
          leftIcon={<Play size={14} />}
          disabled={retryAll.isPending || filtered.length === 0}
          onClick={() => retryAll.mutate()}
          className="shrink-0"
        >
          {retryAll.isPending ? 'Queuing…' : 'Retry all'}
        </Button>
        <div className="flex flex-col sm:flex-row gap-2 flex-1 min-w-0">
          <JobSearchBar value={search} onChange={setSearch} />
          <FilterSelect value={platform} onChange={setPlatform} aria-label="Platform">
            {PLATFORMS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
          </FilterSelect>
        </div>
      </div>

      <JobListPanel>
        {isLoading && <JobListSkeleton />}
        {!isLoading && filtered.length === 0 && (
          <JobListEmpty message="No manual-step jobs right now." action={{ label: 'View top matches', to: '/jobs/top-matches' }} />
        )}
        {filtered.map(job => (
          <CannotApplyJobItem
            key={job.id}
            job={job}
            halalEnabled={halalEnabled}
            onRetry={id => retry.mutate(id)}
            onRequeue={id => requeue.mutate(id)}
            onMarkApplied={id => markApplied.mutate(id)}
            onDelete={id => deleteMutation.mutate(id)}
            actionPending={actionPending}
          />
        ))}
      </JobListPanel>

      {hasNextPage && <LoadMoreButton loading={isFetchingNextPage} onClick={() => fetchNextPage()} />}
      <ListFooter shown={filtered.length} />
    </JobListPage>
  )
}
