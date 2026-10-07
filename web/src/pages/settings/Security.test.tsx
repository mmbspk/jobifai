import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { SecuritySettingsPage } from './Security'

// Use inline vi.fn() to avoid vitest hoisting/TDZ issues.
vi.mock('../../api/user', () => ({
  userApi: { changePassword: vi.fn() },
}))

// mockAuthState is read lazily (only when useAuth() is called), so it's safe here.
let mockAuthState: { user: object | null } = { user: { has_password: true, has_google: false } }

vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => mockAuthState,
}))

import { userApi } from '../../api/user'

beforeEach(() => {
  vi.mocked(userApi.changePassword).mockReset()
  mockAuthState = { user: { has_password: true, has_google: false } }
})

describe('Security settings page', () => {
  it('shows password-change form for email/password account', () => {
    render(<SecuritySettingsPage />)
    expect(screen.getByLabelText(/current password/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/^new password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /update password/i })).toBeInTheDocument()
  })

  it('shows Google-only message and NOT a password form for Google accounts', () => {
    mockAuthState = { user: { has_password: false, has_google: true } }
    render(<SecuritySettingsPage />)
    expect(screen.getByText(/google sign-in/i)).toBeInTheDocument()
    expect(screen.queryByLabelText(/current password/i)).not.toBeInTheDocument()
  })

  it('Google-only message does NOT link to forgot-password', () => {
    mockAuthState = { user: { has_password: false, has_google: true } }
    render(<SecuritySettingsPage />)
    const links = screen.queryAllByRole('link')
    const fpLink = links.find(l => (l as HTMLAnchorElement).href?.includes('forgot-password'))
    expect(fpLink).toBeUndefined()
  })

  it('rejects mismatched passwords before calling API', async () => {
    const user = userEvent.setup()
    render(<SecuritySettingsPage />)

    await user.type(screen.getByLabelText(/current password/i), 'oldpass99')
    await user.type(screen.getByLabelText(/^new password/i), 'newpass99')
    await user.type(screen.getByLabelText(/confirm/i), 'mismatch99')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /update password/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/do not match/i)
    expect(userApi.changePassword).not.toHaveBeenCalled()
  })

  it('calls changePassword and shows success on valid submission', async () => {
    vi.mocked(userApi.changePassword).mockResolvedValue({ message: 'ok' })
    const user = userEvent.setup()
    render(<SecuritySettingsPage />)

    await user.type(screen.getByLabelText(/current password/i), 'oldpass99')
    await user.type(screen.getByLabelText(/^new password/i), 'newpass99')
    await user.type(screen.getByLabelText(/confirm/i), 'newpass99')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /update password/i }))
    })

    expect(userApi.changePassword).toHaveBeenCalledWith('oldpass99', 'newpass99')
    expect(screen.getByRole('status')).toHaveTextContent(/updated successfully/i)
  })
})
