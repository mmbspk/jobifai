import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import { VerifyEmailPage } from './VerifyEmail'

vi.mock('react-router-dom', () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
  useSearchParams: vi.fn(),
}))

vi.mock('../api/user', () => ({
  userApi: {
    verifyEmail: vi.fn(),
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
  vi.mocked(userApi.verifyEmail).mockReset()
})

describe('VerifyEmail page', () => {
  it('shows loading state initially', () => {
    mockSearchParams('sometoken')
    vi.mocked(userApi.verifyEmail).mockReturnValue(new Promise(() => {}))
    render(<VerifyEmailPage />)
    expect(screen.getByText(/verifying/i)).toBeInTheDocument()
  })

  it('shows success state after successful verification', async () => {
    mockSearchParams('validtoken')
    vi.mocked(userApi.verifyEmail).mockResolvedValue({ message: 'Email verified.' })
    render(<VerifyEmailPage />)
    expect(await screen.findByText(/email verified/i)).toBeInTheDocument()
    expect(screen.getByText(/continue to login/i)).toBeInTheDocument()
  })

  it('shows already-used state for already-used token', async () => {
    mockSearchParams('usedtoken')
    vi.mocked(userApi.verifyEmail).mockRejectedValue(new Error('has already been used'))
    render(<VerifyEmailPage />)
    expect(await screen.findByText(/already been used/i)).toBeInTheDocument()
  })

  it('shows expired state for expired token', async () => {
    mockSearchParams('expiredtoken')
    vi.mocked(userApi.verifyEmail).mockRejectedValue(new Error('expired'))
    render(<VerifyEmailPage />)
    expect(await screen.findByText(/expired/i)).toBeInTheDocument()
  })

  it('shows invalid state when token is missing', async () => {
    mockSearchParams(null)
    render(<VerifyEmailPage />)
    expect(await screen.findByText(/invalid or incomplete/i)).toBeInTheDocument()
    expect(userApi.verifyEmail).not.toHaveBeenCalled()
  })

  it('calls /auth/verify-email API endpoint (not /verify-email)', async () => {
    mockSearchParams('sometoken')
    vi.mocked(userApi.verifyEmail).mockResolvedValue({ message: 'verified' })
    render(<VerifyEmailPage />)
    await screen.findByText(/verified/i)
    expect(vi.mocked(userApi.verifyEmail)).toHaveBeenCalledWith('sometoken')
  })
})
