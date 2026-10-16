import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { TopMatches } from './TopMatches'
import { jobsApi } from '../api/jobs'

vi.mock('../api/jobs', () => ({ jobsApi: { topMatches: vi.fn(), deletePendingReview: vi.fn(), markApplied: vi.fn(), blacklistCompany: vi.fn() } }))
vi.mock('../api/settings', () => ({ settingsApi: { general: { get: vi.fn().mockResolvedValue({}) } } }))

beforeEach(() => { vi.clearAllMocks(); vi.mocked(jobsApi.topMatches).mockResolvedValue([]) })

it('requests scored matches without a verification bypass', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><TopMatches /></QueryClientProvider>)
  await waitFor(() => expect(jobsApi.topMatches).toHaveBeenCalledWith({ limit: 50, offset: 0 }))
  expect(screen.getByRole('heading', { name: 'Top Matches' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Needs verification' })).not.toBeInTheDocument()
  expect(screen.queryByText('Why it matches')).not.toBeInTheDocument()
  client.clear()
})
