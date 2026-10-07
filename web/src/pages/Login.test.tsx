import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { Login } from './Login'

vi.mock('../contexts/AuthContext', () => ({
  useAuth: () => ({
    user: null,
    loading: false,
    login: vi.fn(),
    googleLoginUrl: '',
  }),
}))

vi.mock('../lib/postAuthRedirect', () => ({
  resolvePostAuthDestination: () => '/',
  clearPostAuthRedirect: vi.fn(),
  savePostAuthRedirect: vi.fn(),
}))

function renderLogin(state?: object) {
  return render(
    <MemoryRouter initialEntries={[{ pathname: '/login', state }]}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/forgot-password" element={<div>forgot password</div>} />
        <Route path="/register" element={<div>register</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Login page — reset success banner', () => {
  it('shows reset-success banner when navigated from reset flow', () => {
    renderLogin({ resetSuccess: true })
    expect(screen.getByRole('status')).toHaveTextContent(/password has been reset successfully/i)
  })

  it('does not show reset-success banner without the state flag', () => {
    renderLogin()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('does not show the banner when resetSuccess is false', () => {
    renderLogin({ resetSuccess: false })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
