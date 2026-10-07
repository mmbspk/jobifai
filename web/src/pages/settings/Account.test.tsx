import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { AccountSettingsPage } from './Account'

vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
}))

vi.mock('../../api/user', () => ({
  userApi: {
    requestEmailChange: vi.fn(),
    deleteAccount: vi.fn(),
  },
}))

const mockUpdateMe = vi.fn()
const mockForceLogout = vi.fn()
let mockAuthState: { user: object | null; updateMe: typeof mockUpdateMe; forceLogout: typeof mockForceLogout }

vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => mockAuthState,
}))

import { userApi } from '../../api/user'

beforeEach(() => {
  vi.mocked(userApi.requestEmailChange).mockReset()
  vi.mocked(userApi.deleteAccount).mockReset()
  mockUpdateMe.mockReset()
  mockForceLogout.mockReset()
  mockAuthState = {
    user: { id: '1', email: 'user@example.com', display_name: 'Test User', has_password: true, has_google: false, pending_email: null },
    updateMe: mockUpdateMe,
    forceLogout: mockForceLogout,
  }
})

describe('Account settings page', () => {
  it('renders profile form with current display name', () => {
    render(<AccountSettingsPage />)
    expect(screen.getByLabelText(/display name/i)).toHaveValue('Test User')
    expect(screen.getByRole('button', { name: /save profile/i })).toBeInTheDocument()
  })

  it('calls updateMe with trimmed display name on submit', async () => {
    mockUpdateMe.mockResolvedValue(undefined)
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    const nameInput = screen.getByLabelText(/display name/i)
    await user.clear(nameInput)
    await user.type(nameInput, '  Updated Name  ')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /save profile/i }))
    })

    expect(mockUpdateMe).toHaveBeenCalledWith('Updated Name', '')
  })

  it('shows error when display name is empty', async () => {
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    await user.clear(screen.getByLabelText(/display name/i))

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /save profile/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/required/i)
    expect(mockUpdateMe).not.toHaveBeenCalled()
  })

  it('renders email change section for password accounts', () => {
    render(<AccountSettingsPage />)
    expect(screen.getByLabelText(/new email address/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /request email change/i })).toBeInTheDocument()
  })

  it('shows pending email indicator when pending_email is set', () => {
    mockAuthState.user = { ...mockAuthState.user as object, pending_email: 'pending@example.com' }
    render(<AccountSettingsPage />)
    expect(screen.getByText(/pending@example.com/)).toBeInTheDocument()
    expect(screen.getByText(/verification pending/i)).toBeInTheDocument()
  })

  it('calls requestEmailChange and shows success state', async () => {
    vi.mocked(userApi.requestEmailChange).mockResolvedValue({ message: 'sent' })
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    await user.type(screen.getByLabelText(/new email address/i), 'new@example.com')
    // disambiguate from the delete-account password field by id
    await user.type(document.getElementById('email-password')!, 'pass123')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /request email change/i }))
    })

    expect(userApi.requestEmailChange).toHaveBeenCalledWith('new@example.com', 'pass123')
    expect(screen.getByText(/verification email sent/i)).toBeInTheDocument()
  })

  it('shows error when email change fails', async () => {
    vi.mocked(userApi.requestEmailChange).mockRejectedValue(new Error('Email already in use'))
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    await user.type(screen.getByLabelText(/new email address/i), 'taken@example.com')
    await user.type(document.getElementById('email-password')!, 'pass123')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /request email change/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/email already in use/i)
  })

  it('shows Google-only message instead of email change form', () => {
    mockAuthState.user = { id: '1', email: 'user@google.com', display_name: 'Google User', has_password: false, has_google: true, pending_email: null }
    render(<AccountSettingsPage />)
    expect(screen.getByText(/google sign-in/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /request email change/i })).not.toBeInTheDocument()
  })

  it('renders danger zone with password field for password accounts', () => {
    render(<AccountSettingsPage />)
    expect(document.getElementById('delete-password')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /delete my account/i })).toBeInTheDocument()
  })

  it('renders danger zone with DELETE confirmation for Google-only accounts', () => {
    mockAuthState.user = { id: '1', email: 'user@google.com', display_name: 'Google User', has_password: false, has_google: true, pending_email: null }
    render(<AccountSettingsPage />)
    expect(screen.getByPlaceholderText('DELETE')).toBeInTheDocument()
  })

  it('calls deleteAccount and forceLogout on successful deletion', async () => {
    vi.mocked(userApi.deleteAccount).mockResolvedValue({ message: 'deleted' })
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    // Use the delete-password field specifically (disambiguated by id)
    await user.type(document.getElementById('delete-password')!, 'pass123')

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /delete my account/i }))
    })

    expect(userApi.deleteAccount).toHaveBeenCalledWith({ currentPassword: 'pass123' })
    expect(mockForceLogout).toHaveBeenCalled()
  })

  it('shows error when password is missing for deletion', async () => {
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /delete my account/i }))
    })

    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(userApi.deleteAccount).not.toHaveBeenCalled()
  })

  it('requires DELETE text for Google-only account deletion', async () => {
    mockAuthState.user = { id: '1', email: 'user@google.com', display_name: 'Google User', has_password: false, has_google: true, pending_email: null }
    const user = userEvent.setup()
    render(<AccountSettingsPage />)

    await user.type(screen.getByPlaceholderText('DELETE'), 'delete') // lowercase — should fail

    await act(async () => {
      await user.click(screen.getByRole('button', { name: /delete my account/i }))
    })

    expect(screen.getByRole('alert')).toHaveTextContent(/type DELETE/i)
    expect(userApi.deleteAccount).not.toHaveBeenCalled()
  })
})
