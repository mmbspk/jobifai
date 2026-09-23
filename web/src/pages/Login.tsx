import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import { Button } from '../components/Button'
import { Input } from '../components/ui/input'
import { AuthDivider, AuthLayout, GoogleSignInButton } from '../components/auth/AuthLayout'

export function Login() {
  const { login, googleLoginUrl } = useAuth()
  const navigate = useNavigate()
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
      navigate('/', { replace: true })
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
          <Link to="/register" className="text-[var(--color-accent)] hover:underline font-medium">
            Create account
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-4">
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
        <Button type="submit" variant="primary" fullWidth loading={busy} size="lg">
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>

      <AuthDivider />
      <GoogleSignInButton href={googleLoginUrl} />
    </AuthLayout>
  )
}
