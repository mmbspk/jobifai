import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ResetPassword } from './ResetPassword'

// Use inline vi.fn() to avoid hoisting/TDZ issues.
vi.mock('../api/user', () => ({
  userApi: { resetPassword: vi.fn() },
}))

// Import AFTER mock so vi.mocked works correctly.
import { userApi } from '../api/user'

function renderWithToken(token: string) {
  const path = token ? `/reset-password?token=${encodeURIComponent(token)}` : '/reset-password'
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/reset-password" element={<ResetPassword />} />
        {/* Destination route — appears on successful navigation */}
        <Route path="/login" element={<div data-testid="login-page">login</div>} />
        <Route path="/forgot-password" element={<div>forgot password</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(userApi.resetPassword).mockReset()
})

describe('ResetPassword page', () => {
  it('shows error when token is missing from URL', () => {
    renderWithToken('')
    expect(screen.getByText(/missing or invalid/i)).toBeInTheDocument()
    expect(screen.queryByLabelText(/new password/i)).not.toBeInTheDocument()
  })

  it('renders password form when token is present', () => {
    renderWithToken('valid-token-abc')
    expect(screen.getAllByLabelText(/new password/i).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /set new password/i })).toBeInTheDocument()
  })

  it('rejects mismatched passwords before calling API', async () => {
    renderWithToken('valid-token-abc')
    const user = userEvent.setup()

    const [newPass, confirm] = screen.getAllByLabelText(/new password/i)
    await user.type(newPass, 'password123')
    await user.type(confirm, 'different456')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /set new password/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/do not match/i)
    expect(userApi.resetPassword).not.toHaveBeenCalled()
  })

  it('calls resetPassword API with token and password', async () => {
    vi.mocked(userApi.resetPassword).mockResolvedValue({ message: 'ok' })
    renderWithToken('my-token-abc')
    const user = userEvent.setup()

    const [newPass, confirm] = screen.getAllByLabelText(/new password/i)
    await user.type(newPass, 'newpassword99')
    await user.type(confirm, 'newpassword99')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /set new password/i }))
    })

    expect(userApi.resetPassword).toHaveBeenCalledWith('my-token-abc', 'newpassword99')
  })

  it('navigates to login page on successful reset', async () => {
    vi.mocked(userApi.resetPassword).mockResolvedValue({ message: 'ok' })
    renderWithToken('my-token-xyz')
    const user = userEvent.setup()

    const [newPass, confirm] = screen.getAllByLabelText(/new password/i)
    await user.type(newPass, 'newpassword99')
    await user.type(confirm, 'newpassword99')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /set new password/i }))
    })

    expect(screen.getByTestId('login-page')).toBeInTheDocument()
  })

  it('shows error when backend rejects token', async () => {
    vi.mocked(userApi.resetPassword).mockRejectedValue(new Error('invalid or expired reset token'))
    renderWithToken('bad-token')
    const user = userEvent.setup()

    const [newPass, confirm] = screen.getAllByLabelText(/new password/i)
    await user.type(newPass, 'newpassword99')
    await user.type(confirm, 'newpassword99')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /set new password/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/invalid or expired/i)
  })
})
