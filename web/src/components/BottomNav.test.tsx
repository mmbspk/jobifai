import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi } from 'vitest'
import { BottomNav } from './BottomNav'
import { botApi } from '../api/bot'

vi.mock('../api/bot', () => ({
  botApi: {
    reviewPending: vi.fn().mockResolvedValue([]),
  },
}))

const mockedReviewPending = vi.mocked(botApi.reviewPending)

function Wrapper({ children }: Readonly<{ children: React.ReactNode }>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  mockedReviewPending.mockResolvedValue([])
})

test('renders all navigation labels', () => {
  render(<BottomNav />, { wrapper: Wrapper })
  expect(screen.getByText('Home')).toBeInTheDocument()
  expect(screen.getByText('Jobs')).toBeInTheDocument()
  expect(screen.getByText('Review')).toBeInTheDocument()
  expect(screen.getByText('Generate')).toBeInTheDocument()
  expect(screen.getByText('Settings')).toBeInTheDocument()
})

test('opens jobs menu with job routes', async () => {
  const user = userEvent.setup()
  render(<BottomNav />, { wrapper: Wrapper })

  await user.click(screen.getByRole('button', { name: 'Jobs' }))

  expect(screen.getByText('Applied')).toBeInTheDocument()
  expect(screen.getByText('Top Matches')).toBeInTheDocument()
  expect(screen.getByText('Skipped')).toBeInTheDocument()
  expect(screen.getByText('Cannot Apply')).toBeInTheDocument()
})

test('keeps the mobile navigation focused on the five primary destinations', () => {
  render(<BottomNav />, { wrapper: Wrapper })
  expect(screen.getAllByRole('link')).toHaveLength(4)
  expect(screen.getByRole('button', { name: 'Jobs' })).toBeInTheDocument()
})

test('shows pending badge count when there are pending reviews', async () => {
  mockedReviewPending.mockResolvedValue(
    Array.from({ length: 3 }, (_, i) => ({ job_id: String(i) })) as never,
  )
  render(<BottomNav />, { wrapper: Wrapper })

  await waitFor(() => {
    expect(screen.getByText('3')).toBeInTheDocument()
  })
})

test('does not show a badge when there are no pending reviews', async () => {
  mockedReviewPending.mockResolvedValue([])
  render(<BottomNav />, { wrapper: Wrapper })

  await waitFor(() => expect(mockedReviewPending).toHaveBeenCalled())
  expect(screen.queryByText('3')).not.toBeInTheDocument()
})

test('caps badge at 9 for more than 9 pending reviews', async () => {
  mockedReviewPending.mockResolvedValue(
    Array.from({ length: 10 }, (_, i) => ({ job_id: String(i) })) as never,
  )
  render(<BottomNav />, { wrapper: Wrapper })

  await waitFor(() => {
    expect(screen.getByText('9')).toBeInTheDocument()
  })
})
