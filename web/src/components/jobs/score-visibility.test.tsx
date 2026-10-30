import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AppliedJobItem } from './applied-job-item'
import { SkippedJobItem } from './skipped-job-item'
import { CannotApplyJobItem } from './cannot-apply-job-item'
import { TopMatchJobItem } from './top-match-job-item'
import { ReviewCard } from '../review/ReviewCard'
import { Dashboard } from '../../pages/Dashboard'

vi.mock('../review/ReviewDocumentsPanel', () => ({ ReviewDocumentsPanel: () => null }))
vi.mock('../../hooks/useBot', () => ({ useBot: () => ({ status: null, start: { isPending: false }, stop: { isPending: false }, pause: { isPending: false }, resume: { isPending: false } }) }))
vi.mock('../../hooks/useLogs', () => ({ useLogs: () => ({ lines: [], connected: false, displayedRunId: '' }) }))
vi.mock('../../hooks/useSetupReadiness', () => ({ useSetupReadiness: () => ({ steps: [], ready: true }) }))
vi.mock('../../hooks/useQuota', () => ({ useQuota: () => ({ aiDisabled: false }) }))
vi.mock('../../contexts/AuthContext', () => ({ useAuth: () => ({ user: null }) }))
vi.mock('../../api/jobs', () => ({ jobsApi: { stats: vi.fn().mockResolvedValue({}), topMatches: vi.fn() } }))
vi.mock('../../api/bot', () => ({ botApi: { reviewPending: vi.fn().mockResolvedValue([]) } }))
import { jobsApi } from '../../api/jobs'

const noop = () => {}
const base = { id: '1', job_id: '1', role: 'Role', company: 'Company', platform: 'seek' as const,
  link: 'https://au.seek.com/job/1', applied_at: '2026-10-30', viewed_at: '2026-10-30', created_at: '2026-10-30',
  easy_apply: true, skip_reason: 'score 0 < 7' }
afterEach(cleanup)

for (const view of ['applied', 'skipped', 'cannot apply', 'top match', 'review', 'dashboard']) {
  describe(view, () => {
    for (const score of [0, null, undefined, 1, 4, 7, 10]) {
      it(`renders the score badge correctly for ${score}`, async () => {
        // API payloads can contain null despite the optional numeric TS field.
        const job = { ...base, suitability_score: score as number | undefined }
        vi.mocked(jobsApi.topMatches).mockResolvedValue([job])
        const content = view === 'applied' ? <AppliedJobItem job={job} onDelete={noop} deletePending={false} />
          : view === 'skipped' ? <SkippedJobItem job={job} halalEnabled={false} onDelete={noop} deletePending={false} />
          : view === 'cannot apply' ? <CannotApplyJobItem job={job} halalEnabled={false} onRetry={noop} onMarkApplied={noop} onDelete={noop} actionPending={false} />
          : view === 'top match' ? <TopMatchJobItem job={job} halalEnabled={false} onDelete={noop} onMarkApplied={noop} onBlacklist={noop} onBlacklistAndClear={noop} pending={false} />
          : view === 'review' ? <ReviewCard review={job} halalEnabled={false} onApprove={noop} onReject={noop} />
          : <Dashboard />
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
        const { container, unmount } = render(<QueryClientProvider client={client}><MemoryRouter>{content}</MemoryRouter></QueryClientProvider>)
        if (view === 'dashboard') await screen.findByText('Role')
        const badge = container.querySelector('[title*="match · score"]')
        if (score == null) expect(badge).toBeNull()
        else {
          expect(badge).toHaveTextContent(String(score))
          const band = score <= 3 ? 'danger' : score <= 6 ? 'warn' : score <= 8 ? 'accent' : 'success'
          expect(badge?.className).toContain(`text-[var(--color-${band})]`)
        }
        unmount()
        client.clear()
      })
    }
  })
}
