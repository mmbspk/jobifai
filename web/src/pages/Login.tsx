import { useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  clearPostAuthRedirect,
  resolvePostAuthDestination,
  savePostAuthRedirect,
} from '../lib/postAuthRedirect'
import { useAuth } from '../contexts/AuthContext'
import { Button } from '../components/Button'
import { Input } from '../components/ui/input'
import { AuthDivider, AuthLayout, GoogleSignInButton } from '../components/auth/AuthLayout'

type AuthRedirectState = { from?: { pathname: string; search?: string; hash?: string }; resetSuccess?: boolean }

export function Login() {
  const { login, googleLoginUrl } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const state = location.state as AuthRedirectState | null
  const returnTo = resolvePostAuthDestination(state?.from)
  const resetSuccess = state?.resetSuccess === true
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await login(email, password)
      const dest = resolvePostAuthDestination(state?.from)
      clearPostAuthRedirect()
      navigate(dest, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Welcome back"
      subtitle="Continue your job search automation."
      footer={
        <>
          New to Jobifai?{' '}
          <Link to="/register" state={location.state} className="text-[var(--color-accent)] hover:underline font-medium">
            Create account
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {resetSuccess && (
          <p className="text-sm text-green-700 bg-green-50 border border-green-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-green-300 dark:bg-green-950/40 dark:border-green-800/40" role="status">
            Your password has been reset successfully. Sign in with your new password.
          </p>
        )}
        {error && (
          <p className="text-sm text-[var(--color-danger)] bg-[var(--color-danger-soft)] border border-[var(--color-danger)]/20 rounded-[var(--radius-md)] px-3 py-2" role="alert">
            {error}
          </p>
        )}
        <Input
          label="Email"
          type="email"
          required
          autoComplete="email"
          value={email}
          onChange={e => setEmail(e.target.value)}
        />
        <Input
          label="Password"
          type="password"
          required
          autoComplete="current-password"
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
        <div className="text-right -mt-2">
          <Link to="/forgot-password" className="text-xs text-[var(--color-text-muted)] hover:text-[var(--color-accent)] hover:underline">
            Forgot password?
          </Link>
        </div>
        <Button type="submit" variant="primary" fullWidth loading={busy} size="lg">
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>

      <AuthDivider />
      <GoogleSignInButton href={googleLoginUrl} onBeforeNavigate={() => savePostAuthRedirect(returnTo)} />
    </AuthLayout>
  )
}
