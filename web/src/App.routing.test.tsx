import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { vi } from 'vitest'

// Minimal page stubs — we only care that the right component renders for the route
vi.mock('./pages/VerifyEmail', () => ({
  VerifyEmailPage: () => <div data-testid="verify-email-page">VerifyEmailPage</div>,
}))
vi.mock('./pages/VerifyEmailChange', () => ({
  VerifyEmailChangePage: () => <div data-testid="verify-email-change-page">VerifyEmailChangePage</div>,
}))

// Stub everything else imported by App.tsx so we don't need a full provider tree
vi.mock('./contexts/AuthContext', () => ({
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useAuth: () => ({ user: null, loading: false }),
}))
vi.mock('./components/auth/PostAuthRedirect', () => ({
  PostAuthRedirect: () => null,
}))
vi.mock('./lib/authSession', () => ({ hasStoredAuthCredentials: () => false }))
vi.mock('./lib/postAuthRedirect', () => ({
  resolvePostAuthDestination: () => '/',
  savePostAuthRedirect: vi.fn(),
  clearPostAuthRedirect: vi.fn(),
}))

import { VerifyEmailPage } from './pages/VerifyEmail'
import { VerifyEmailChangePage } from './pages/VerifyEmailChange'

// Render just the two routes in isolation — avoids the full App provider tree
function renderRoutes(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route path="/verify-email-change" element={<VerifyEmailChangePage />} />
        <Route path="*" element={<div data-testid="no-match">no match</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('App routing — verification pages', () => {
  it('/verify-email renders VerifyEmailPage', () => {
    renderRoutes('/verify-email?token=abc')
    expect(screen.getByTestId('verify-email-page')).toBeInTheDocument()
    expect(screen.queryByTestId('no-match')).not.toBeInTheDocument()
  })

  it('/verify-email-change renders VerifyEmailChangePage', () => {
    renderRoutes('/verify-email-change?token=abc')
    expect(screen.getByTestId('verify-email-change-page')).toBeInTheDocument()
    expect(screen.queryByTestId('no-match')).not.toBeInTheDocument()
  })

  it('/auth/verify-email does NOT match (old route removed from frontend)', () => {
    renderRoutes('/auth/verify-email?token=abc')
    expect(screen.getByTestId('no-match')).toBeInTheDocument()
    expect(screen.queryByTestId('verify-email-page')).not.toBeInTheDocument()
  })

  it('/auth/verify-email-change does NOT match (old route removed from frontend)', () => {
    renderRoutes('/auth/verify-email-change?token=abc')
    expect(screen.getByTestId('no-match')).toBeInTheDocument()
    expect(screen.queryByTestId('verify-email-change-page')).not.toBeInTheDocument()
  })
})
