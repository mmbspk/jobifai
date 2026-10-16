import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { TopMatches } from './TopMatches'
import { jobsApi } from '../api/jobs'

vi.mock('../api/jobs', () => ({ jobsApi: { topMatches: vi.fn(), deletePendingReview: vi.fn(), markApplied: vi.fn(), blacklistCompany: vi.fn() } }))
vi.mock('../api/settings', () => ({ settingsApi: { general: { get: vi.fn().mockResolvedValue({}) } } }))

beforeEach(() => { vi.clearAllMocks(); vi.mocked(jobsApi.topMatches).mockResolvedValue([]) })

it('keeps verification holds in a separate view and query cache', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><TopMatches /></QueryClientProvider>)
  await waitFor(() => expect(jobsApi.topMatches).toHaveBeenCalledWith({ limit: 50, offset: 0, needs_verification: false }))
  expect(screen.getByRole('heading', { name: 'Top Matches' })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Needs verification' }))
  await waitFor(() => expect(jobsApi.topMatches).toHaveBeenCalledWith({ limit: 50, offset: 0, needs_verification: true }))
  expect(screen.getByRole('heading', { name: 'Needs verification' })).toBeInTheDocument()
  expect(screen.queryByText('Why it matches')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Top Matches' }))
  expect(screen.getByRole('heading', { name: 'Top Matches' })).toBeInTheDocument()
  client.clear()
})
