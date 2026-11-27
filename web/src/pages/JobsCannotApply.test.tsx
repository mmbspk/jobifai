import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import { JobsCannotApply } from './JobsCannotApply'
vi.mock('../contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'test-user' }, loading: false }) }))
import { jobsApi } from '../api/jobs'

vi.mock('../api/settings', () => ({ settingsApi: { general: { get: vi.fn(async () => ({})) } } }))
vi.mock('../api/jobs', () => ({ jobsApi: {
  cannotApply: vi.fn(), retryCannotApply: vi.fn(), retryAllCannotApply: vi.fn(),
  markAppliedFromCannotApply: vi.fn(), deleteSkipped: vi.fn(),
} }))

function Destination() {
  const location = useLocation()
  return <output>{location.pathname + location.search}</output>
}

beforeEach(() => vi.clearAllMocks())

it.each(['https://au.seek.com/job/95969588', 'https://www.linkedin.com/jobs/view/4494759546/'])('opens Re-evaluate without removing or queuing %s', async link => {
  vi.mocked(jobsApi.cannotApply).mockResolvedValue([{
    id: 'job-1', company: 'Company', role: 'Engineer', platform: link.includes('seek') ? 'seek' : 'linkedin',
    link, skip_reason: 'easy apply: description unavailable', viewed_at: new Date().toISOString(),
  }])
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={['/jobs/cannot-apply']}>
    <Routes><Route path="/jobs/cannot-apply" element={<JobsCannotApply />} /><Route path="/generate" element={<Destination />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
  await userEvent.setup().click(await screen.findByRole('button', { name: 'Re-evaluate' }))
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('/generate?tab=apply&url=' + encodeURIComponent(link)))
  expect(jobsApi.retryCannotApply).not.toHaveBeenCalled()
  expect(jobsApi.deleteSkipped).not.toHaveBeenCalled()
})
