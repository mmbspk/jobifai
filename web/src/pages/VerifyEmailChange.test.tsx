import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import { VerifyEmailChangePage } from './VerifyEmailChange'

vi.mock('react-router-dom', () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
  useSearchParams: vi.fn(),
}))

vi.mock('../api/user', () => ({
  userApi: {
    verifyEmailChange: vi.fn(),
  },
}))

vi.mock('../components/auth/AuthLayout', () => ({
  AuthLayout: ({ title, children, footer }: { title: string; children: React.ReactNode; footer?: React.ReactNode }) => (
    <div>
      <h1>{title}</h1>
      {children}
      {footer}
    </div>
  ),
}))

import { useSearchParams } from 'react-router-dom'
import { userApi } from '../api/user'

function mockSearchParams(token: string | null) {
  vi.mocked(useSearchParams).mockReturnValue([
    { get: (key: string) => (key === 'token' ? token : null) } as unknown as URLSearchParams,
    vi.fn(),
  ])
}

beforeEach(() => {
  vi.mocked(userApi.verifyEmailChange).mockReset()
})

describe('VerifyEmailChange page', () => {
  it('shows loading state initially', () => {
    mockSearchParams('sometoken')
    vi.mocked(userApi.verifyEmailChange).mockReturnValue(new Promise(() => {})) // never resolves
    render(<VerifyEmailChangePage />)
    expect(screen.getByText(/verifying/i)).toBeInTheDocument()
  })

  it('shows success state after successful verification', async () => {
    mockSearchParams('validtoken')
    vi.mocked(userApi.verifyEmailChange).mockResolvedValue({ message: '', code: 'success' })
    render(<VerifyEmailChangePage />)
    expect(await screen.findByText(/updated successfully/i)).toBeInTheDocument()
    expect(screen.getByText(/continue to login/i)).toBeInTheDocument()
  })

  it('shows expired state for expired token', async () => {
    mockSearchParams('expiredtoken')
    vi.mocked(userApi.verifyEmailChange).mockRejectedValue(Object.assign(new Error('expired'), { code: 'expired' }))
    render(<VerifyEmailChangePage />)
    expect(await screen.findByText(/expired/i)).toBeInTheDocument()
  })

  it('shows invalid state for bad token', async () => {
    mockSearchParams('badtoken')
    vi.mocked(userApi.verifyEmailChange).mockRejectedValue(Object.assign(new Error('not found'), { code: 'not_found' }))
    render(<VerifyEmailChangePage />)
    expect(await screen.findByText(/invalid or has already been used/i)).toBeInTheDocument()
  })

  it('shows invalid state when token is missing from URL', async () => {
    mockSearchParams(null)
    render(<VerifyEmailChangePage />)
    expect(await screen.findByText(/invalid or has already been used/i)).toBeInTheDocument()
    expect(userApi.verifyEmailChange).not.toHaveBeenCalled()
  })

  it('shows error state for unknown errors', async () => {
    mockSearchParams('sometoken')
    vi.mocked(userApi.verifyEmailChange).mockRejectedValue(Object.assign(new Error('Server error'), { code: '' }))
    render(<VerifyEmailChangePage />)
    expect(await screen.findByText(/Server error/i)).toBeInTheDocument()
  })
})
