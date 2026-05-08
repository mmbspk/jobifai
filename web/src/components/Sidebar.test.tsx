import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi } from 'vitest'
import { Sidebar } from './Sidebar'
import { botApi } from '../api/bot'

vi.mock('../api/bot', () => ({
  botApi: {
    reviewPending: vi.fn().mockResolvedValue([]),
  },
}))

vi.mock('../api/usage', () => ({
  usageApi: {
    session: vi.fn().mockResolvedValue(null),
  },
}))

vi.mock('../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', toggle: vi.fn() }),
}))

const mockLogout = vi.fn().mockResolvedValue(undefined)
const mockUseAuth = vi.fn()

vi.mock('../contexts/AuthContext', () => ({
  useAuth: () => mockUseAuth(),
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
  mockLogout.mockResolvedValue(undefined)
  mockUseAuth.mockReturnValue({
    user: { email: 'test@example.com', display_name: 'Test User' },
    logout: mockLogout,
  })
})

test('renders all navigation items', () => {
  render(<Sidebar />, { wrapper: Wrapper })
  expect(screen.getByText('Dashboard')).toBeInTheDocument()
  expect(screen.getByText('Applied')).toBeInTheDocument()
  expect(screen.getByText('Skipped')).toBeInTheDocument()
  expect(screen.getByText('Cannot Apply')).toBeInTheDocument()
  expect(screen.getByText('Top Matches')).toBeInTheDocument()
  expect(screen.getByText('Review')).toBeInTheDocument()
  expect(screen.getByText('Generate')).toBeInTheDocument()
  expect(screen.getByText('Settings')).toBeInTheDocument()
})

test('shows user display name in footer', () => {
  render(<Sidebar />, { wrapper: Wrapper })
  expect(screen.getByText('Test User')).toBeInTheDocument()
})

test('shows brand name', () => {
  render(<Sidebar />, { wrapper: Wrapper })
  expect(screen.getByText('Jobifai')).toBeInTheDocument()
})

test('shows pending badge count when there are pending reviews', async () => {
  mockedReviewPending.mockResolvedValue(
    Array.from({ length: 5 }, (_, i) => ({ job_id: String(i) })) as never,
  )
  render(<Sidebar />, { wrapper: Wrapper })

  await waitFor(() => {
    expect(screen.getByText('5')).toBeInTheDocument()
  })
})

test('caps badge at 9+ for more than 9 pending reviews', async () => {
  mockedReviewPending.mockResolvedValue(
    Array.from({ length: 12 }, (_, i) => ({ job_id: String(i) })) as never,
  )
  render(<Sidebar />, { wrapper: Wrapper })

  await waitFor(() => {
    expect(screen.getByText('9+')).toBeInTheDocument()
  })
})

test('calls logout when sign-out button is clicked', async () => {
  const user = userEvent.setup()
  render(<Sidebar />, { wrapper: Wrapper })

  await user.click(screen.getByTitle('Sign out'))

  expect(mockLogout).toHaveBeenCalledTimes(1)
})

test('falls back to email initial when display_name is absent', () => {
  mockUseAuth.mockReturnValue({
    user: { email: 'alice@example.com', display_name: '' },
    logout: mockLogout,
  })
  render(<Sidebar />, { wrapper: Wrapper })
  expect(screen.getByText('alice@example.com')).toBeInTheDocument()
})
