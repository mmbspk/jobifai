import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import { Layout } from '../Layout'
import { Dashboard } from '../../pages/Dashboard'
import { ApplicationSettingsPage } from '../../pages/settings/Application'
import { Review } from '../../pages/Review'
import { ManualReviewRoute } from './ManualReviewRoute'
import { settingsApi } from '../../api/settings'
import { botApi } from '../../api/bot'
import { generalSettingsQueryKey } from '../../hooks/useGeneralSettings'
import { reviewPendingQueryKey, usePendingReviews } from '../../hooks/useManualReview'
import type { GeneralSettings, PendingReview } from '../../types'

const auth = vi.hoisted(() => ({ user: { id: 'account-a', display_name: 'Sam', email_verified: true }, loading: false }))
vi.mock('../../contexts/AuthContext', () => ({ useAuth: () => auth }))
vi.mock('../../hooks/useTheme', () => ({ useTheme: () => ({ theme: 'dark', toggle: vi.fn() }) }))
vi.mock('../../hooks/useQuota', () => ({ useQuota: () => ({ unlimited: true, aiDisabled: false, data: { unlimited: true } }) }))
vi.mock('../../hooks/useSetupReadiness', () => ({ useSetupReadiness: () => ({ ready: true, loading: false, steps: [], progress: 100, refresh: vi.fn() }) }))
vi.mock('../../hooks/useLogs', () => ({ useLogs: () => ({ lines: [], connected: false, displayedRunId: '', clear: vi.fn(), wipe: vi.fn() }) }))
vi.mock('../../hooks/useApplicationAnswerReviews', () => ({ useApplicationAnswerReviews: () => ({ data: { pages: [{ unread_count: 1, unresolved_count: 1 }] } }) }))
vi.mock('../../api/usage', () => ({ usageApi: { session: vi.fn(async () => null) } }))
vi.mock('../../api/jobs', () => ({ jobsApi: { stats: vi.fn(async () => ({ applied_today: 2, skipped_today: 3, top_matches_count: 1 })), topMatches: vi.fn(async () => []) } }))
vi.mock('../../api/settings', () => ({ settingsApi: { general: { get: vi.fn(), set: vi.fn() }, markets: { list: vi.fn(async () => []) }, applicationAnswers: { markRead: vi.fn() } } }))
vi.mock('../../api/bot', () => ({ botApi: { status: vi.fn(async () => ({ state: 'pending_review', today_count: 2, daily_limit: 5 })), reviewPending: vi.fn(), reviewPrepare: vi.fn(), reviewApprove: vi.fn(), reviewReject: vi.fn(), start: vi.fn(), stop: vi.fn(), pause: vi.fn(), resume: vi.fn() } }))
// Document editing/preparation has its own integration suite; keep the queue's
// real card and approve/reject callbacks here without document network fixtures.
vi.mock('../review/ReviewDocumentsPanel', () => ({ ReviewDocumentsPanel: () => <p>Document editor</p> }))

const queue: PendingReview[] = [{ job_id: 'saved-job', role: 'Engineer', company: 'Existing Queue', platform: 'seek', easy_apply: true, link: 'https://example.com/job', created_at: '', document_refs_json: '{"prepared":true}' }]
const savedSettings = (enabled: boolean): GeneralSettings => ({ require_review_before_submit: enabled, default_resume_market: '', job_suitability_score: 7, max_jobs_per_keyword: 25, halal_job_filter: true, human_behavior: { daily_application_limit: 5 } })

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}

function mount(path = '/', client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  const tree = () => <QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><Routes>
    <Route element={<Layout />}>
      <Route path="/" element={<Dashboard />} />
      <Route path="/settings/application" element={<ApplicationSettingsPage />} />
      <Route path="/review" element={<ManualReviewRoute><Review /></ManualReviewRoute>} />
    </Route>
  </Routes></MemoryRouter></QueryClientProvider>
  const view = render(tree())
  return { client, ...view, rerenderAuth: () => view.rerender(tree()) }
}

beforeEach(() => {
  vi.clearAllMocks()
  window.matchMedia = vi.fn().mockImplementation((media: string) => ({ matches: true, media, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  auth.user = { id: 'account-a', display_name: 'Sam', email_verified: true }
  auth.loading = false
  vi.mocked(settingsApi.general.get).mockResolvedValue(savedSettings(false))
  vi.mocked(settingsApi.general.set).mockResolvedValue(undefined)
  vi.mocked(botApi.reviewPending).mockResolvedValue(queue)
  vi.mocked(botApi.reviewApprove).mockResolvedValue(undefined)
  vi.mocked(botApi.reviewReject).mockResolvedValue(undefined)
})

it('hides Review while loading and redirects a disabled direct route without fetching the queue', async () => {
  const get = deferred<GeneralSettings>()
  vi.mocked(settingsApi.general.get).mockReturnValue(get.promise)
  mount('/review')
  expect(screen.getByRole('status')).toHaveTextContent('Loading application settings')
  expect(screen.queryByRole('link', { name: /^Review$/ })).not.toBeInTheDocument()
  expect(botApi.reviewPending).not.toHaveBeenCalled()
  await act(async () => get.resolve(savedSettings(false)))
  expect(await screen.findByText('Automation is waiting')).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Review' })).not.toBeInTheDocument()
  expect(screen.queryByText('Awaiting review')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Open review queue')).not.toBeInTheDocument()
  expect(screen.queryByText(/need review/)).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Stop automation' })).toBeEnabled()
  for (const name of ['Applied', 'Skipped', 'Cannot Apply', 'Top Matches', 'Saved documents', 'Review answers']) {
    expect(screen.getAllByRole('link', { name: new RegExp(name) }).length).toBeGreaterThan(0)
  }
  expect(botApi.reviewPending).not.toHaveBeenCalled()
  expect(botApi.reviewApprove).not.toHaveBeenCalled()
})

it('keeps the initial route closed on settings errors and allows retry', async () => {
  vi.mocked(settingsApi.general.get).mockRejectedValueOnce(new Error('Unavailable'))
  mount('/review')
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load application settings')
  expect(botApi.reviewPending).not.toHaveBeenCalled()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByText('Automation is waiting')).toBeInTheDocument()
})

it('applies only effective saved settings, and restores the preserved queue after off/on', async () => {
  vi.mocked(settingsApi.general.get).mockResolvedValue(savedSettings(true))
  const { client } = mount('/settings/application')
  const user = userEvent.setup()
  const toggle = await screen.findByRole('switch', { name: 'Review before submission' })
  await waitFor(() => expect(toggle).toBeEnabled())
  await screen.findAllByRole('link', { name: /^Review/ })
  await user.click(toggle)
  expect(screen.getAllByRole('link', { name: /^\d*\s*Review\s*\d*$/ })).toHaveLength(2)
  expect(settingsApi.general.set).not.toHaveBeenCalled()
  // POST can succeed before the effective settings GET arrives.
  const effective = deferred<GeneralSettings>()
  vi.mocked(settingsApi.general.get).mockReturnValueOnce(effective.promise)
  await user.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(settingsApi.general.set).toHaveBeenCalledWith(expect.objectContaining({ require_review_before_submit: false })))
  expect(screen.getAllByRole('link', { name: /^\d*\s*Review\s*\d*$/ })).toHaveLength(2)
  await act(async () => effective.resolve(savedSettings(false)))
  await waitFor(() => expect(screen.queryByRole('link', { name: /^Review$/ })).not.toBeInTheDocument())
  expect(client.getQueryData(reviewPendingQueryKey('account-a'))).toEqual(queue)
  // Re-enable and use an actual queue action only after an explicit click.
  vi.mocked(settingsApi.general.get).mockResolvedValue(savedSettings(true))
  await user.click(toggle)
  await user.click(screen.getByRole('button', { name: /Saved|Save changes/ }))
  await user.click((await screen.findAllByRole('link', { name: /^\d*\s*Review\s*\d*$/ }))[0])
  expect(await screen.findByText('Existing Queue')).toBeInTheDocument()
  expect(screen.getByText('Document editor')).toBeInTheDocument()
  expect(botApi.reviewApprove).not.toHaveBeenCalled()
  expect(botApi.reviewReject).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Approve application' }))
  await waitFor(() => expect(botApi.reviewApprove).toHaveBeenCalledWith('saved-job', expect.anything()))
})

it('preserves enabled queue preparation and rejection', async () => {
  vi.mocked(settingsApi.general.get).mockResolvedValue(savedSettings(true))
  vi.mocked(botApi.reviewPending).mockResolvedValue([{ ...queue[0], document_refs_json: undefined }])
  mount('/review')
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Prepare documents' }))
  await waitFor(() => expect(botApi.reviewPrepare).toHaveBeenCalledWith('saved-job', expect.anything()))
  await user.click(screen.getByRole('button', { name: 'Reject' }))
  await waitFor(() => expect(botApi.reviewReject).toHaveBeenCalledWith('saved-job', expect.anything()))
})

it('reports an effective-settings reload failure instead of publishing the unsaved flag', async () => {
  vi.mocked(settingsApi.general.get).mockResolvedValue(savedSettings(true))
  mount('/settings/application')
  const user = userEvent.setup()
  const toggle = await screen.findByRole('switch', { name: 'Review before submission' })
  await waitFor(() => expect(toggle).toBeEnabled())
  await user.click(toggle)
  vi.mocked(settingsApi.general.get).mockRejectedValueOnce(new Error('Settings reload failed'))
  await user.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('Settings reload failed')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Saved' })).not.toBeInTheDocument()
  expect(screen.getAllByRole('link', { name: /^\d*\s*Review\s*\d*$/ })).toHaveLength(2)
  expect(botApi.reviewApprove).not.toHaveBeenCalled()
})

it('never publishes late settings from another account or during authentication', async () => {
  const oldGet = deferred<GeneralSettings>()
  vi.mocked(settingsApi.general.get).mockReturnValueOnce(oldGet.promise)
  const view = mount()
  auth.user = { ...auth.user, id: 'account-b' }
  view.rerenderAuth()
  await waitFor(() => expect(view.client.getQueryData(generalSettingsQueryKey('account-b'))).toEqual(savedSettings(false)))
  await act(async () => oldGet.resolve(savedSettings(true)))
  expect(screen.queryByRole('link', { name: /^Review$/ })).not.toBeInTheDocument()
  expect(botApi.reviewPending).not.toHaveBeenCalled()
  await act(async () => view.client.setQueryData(generalSettingsQueryKey('account-b'), savedSettings(true)))
  await screen.findAllByRole('link', { name: /^Review$/ })
  auth.loading = true
  view.rerenderAuth()
  expect(screen.queryByRole('link', { name: /^Review$/ })).not.toBeInTheDocument()
})

it('masks late queue responses and stops polling when review is disabled', async () => {
  const pending = deferred<PendingReview[]>()
  vi.mocked(botApi.reviewPending).mockReturnValueOnce(pending.promise)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(generalSettingsQueryKey('account-a'), savedSettings(true))
  function PollingQueue() { const query = usePendingReviews(20); return <p>{query.data?.length ?? 0} visible</p> }
  const { rerender } = render(<QueryClientProvider client={client}><PollingQueue /></QueryClientProvider>)
  await waitFor(() => expect(botApi.reviewPending).toHaveBeenCalledTimes(1))
  await act(async () => client.setQueryData(generalSettingsQueryKey('account-a'), savedSettings(false)))
  await act(async () => pending.resolve(queue))
  expect(screen.getByText('0 visible')).toBeInTheDocument()
  await act(async () => { await new Promise(r => setTimeout(r, 70)) })
  expect(botApi.reviewPending).toHaveBeenCalledTimes(1)
  expect(client.getQueryData(reviewPendingQueryKey('account-a'))).toEqual(queue)
  auth.user = { ...auth.user, id: 'account-b' }
  client.setQueryData(generalSettingsQueryKey('account-b'), savedSettings(false))
  rerender(<QueryClientProvider client={client}><PollingQueue /></QueryClientProvider>)
  expect(screen.getByText('0 visible')).toBeInTheDocument()
})
