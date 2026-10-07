import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { EmailVerificationBanner } from './EmailVerificationBanner'

// Mock AuthContext so we can inject a specific user state.
const mockResend = vi.fn()

vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => ({
    user: { email: 'user@example.com', email_verified: false, has_google: false },
    resendVerification: mockResend,
  }),
}))

beforeEach(() => {
  mockResend.mockReset()
})

test('shows resend button for unverified email/password account', () => {
  render(<EmailVerificationBanner />)
  expect(screen.getByRole('button', { name: /resend/i })).toBeInTheDocument()
})

test('shows neutral confirmation message after resend — not "Sent!"', async () => {
  mockResend.mockResolvedValue(undefined)
  const user = userEvent.setup()
  render(<EmailVerificationBanner />)

  await act(async () => {
    await user.click(screen.getByRole('button', { name: /resend/i }))
  })

  // Must show a neutral message (rate-limited requests also get 202)
  expect(screen.queryByText('Sent!')).not.toBeInTheDocument()
  expect(screen.getByText(/request received/i)).toBeInTheDocument()
})
