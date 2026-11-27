import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { TopMatches } from './TopMatches'
vi.mock('../contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'test-user' }, loading: false }) }))
import { jobsApi } from '../api/jobs'
import { settingsApi } from '../api/settings'

vi.mock('../api/jobs', () => ({ jobsApi: { topMatches: vi.fn(), deletePendingReview: vi.fn(), markApplied: vi.fn(), blacklistCompany: vi.fn() } }))
vi.mock('../api/settings', () => ({ settingsApi: { general: { get: vi.fn().mockResolvedValue({}) } } }))

beforeEach(() => { vi.clearAllMocks(); vi.mocked(jobsApi.topMatches).mockResolvedValue([]); vi.mocked(settingsApi.general.get).mockResolvedValue({}) })

it('requests scored matches without a verification bypass', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><TopMatches /></QueryClientProvider>)
  await waitFor(() => expect(jobsApi.topMatches).toHaveBeenCalledWith({ limit: 50, offset: 0 }))
  expect(screen.getByRole('heading', { name: 'Top Matches' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Needs verification' })).not.toBeInTheDocument()
  expect(screen.queryByText('Why it matches')).not.toBeInTheDocument()
  client.clear()
})

it('keeps ethics warnings and job actions visible when manual review is off', async () => {
  vi.mocked(settingsApi.general.get).mockResolvedValue({ require_review_before_submit: false, halal_job_filter: true })
  vi.mocked(jobsApi.topMatches).mockResolvedValue([{ job_id: 'manual-match', company: 'Company', role: 'Engineer', platform: 'seek', easy_apply: false, created_at: '', halal_verdict: { verdict: 'DOUBTFUL', confidence: 'MEDIUM', summary: 'Check employer activities', reasons: ['Potential concern'], caveats: null, scholar_note: null } }])
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><TopMatches /></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Why it matches' }))
  expect(screen.getByText('Employment ethics')).toBeInTheDocument()
  expect(screen.getByText('Check employer activities')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Mark applied/ })).toBeInTheDocument()
  client.clear()
})
