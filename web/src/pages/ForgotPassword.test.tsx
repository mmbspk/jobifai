import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { ForgotPassword } from './ForgotPassword'

vi.mock('../api/user', () => ({
  userApi: { forgotPassword: vi.fn() },
}))

import { userApi } from '../api/user'

function renderPage() {
  return render(
    <MemoryRouter>
      <ForgotPassword />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(userApi.forgotPassword).mockReset()
})

describe('ForgotPassword page', () => {
  it('renders an email input and submit button', () => {
    renderPage()
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /send reset link/i })).toBeInTheDocument()
  })

  it('shows neutral anti-enumeration success message after submit', async () => {
    vi.mocked(userApi.forgotPassword).mockResolvedValue({ message: 'ok' })
    const user = userEvent.setup()
    renderPage()

    await user.type(screen.getByLabelText(/email/i), 'anyone@example.com')
    await act(async () => {
      await user.click(screen.getByRole('button', { name: /send reset link/i }))
    })

    // Must not say "sent" or imply the account exists.
    expect(screen.getByText(/request received/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /send reset link/i })).not.toBeInTheDocument()
  })

  it('shows error banner on network failure', async () => {
    vi.mocked(userApi.forgotPassword).mockRejectedValue(new Error('network error'))
    const user = userEvent.setup()
    renderPage()

    await user.type(screen.getByLabelText(/email/i), 'test@example.com')
    await act(async () => {
      await user.click(screen.getByRole('button', { name: /send reset link/i }))
    })

    expect(screen.getByRole('alert')).toBeInTheDocument()
  })
})
