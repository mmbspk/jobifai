import { render, screen, waitFor } from '@testing-library/react'
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

vi.mock('../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', toggle: vi.fn() }),
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
  expect(screen.getByText('Applied')).toBeInTheDocument()
  expect(screen.getByText('Manual')).toBeInTheDocument()
  expect(screen.getByText('Review')).toBeInTheDocument()
  expect(screen.getByText('Settings')).toBeInTheDocument()
})

test('renders a theme toggle button', () => {
  render(<BottomNav />, { wrapper: Wrapper })
  const toggle = screen.getByRole('button')
  expect(toggle).toBeInTheDocument()
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

  // Give react-query time to resolve before asserting badge is absent
  await waitFor(() => expect(mockedReviewPending).toHaveBeenCalled())
  expect(screen.queryByText(/^\d+$/)).not.toBeInTheDocument()
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
