import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import { Button } from '../components/Button'
import { Input } from '../components/ui/input'
import { AuthDivider, AuthLayout, GoogleSignInButton } from '../components/auth/AuthLayout'

export function Register() {
  const { register, googleLoginUrl } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (password.length < 8) { setError('Password must be at least 8 characters'); return }
    setBusy(true)
    try {
      await register(email, password, name)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Registration failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Create your Jobifai account"
      subtitle="Set up automation that keeps you in control of every submission."
      footer={
        <>
          Already have an account?{' '}
          <Link to="/login" className="text-[var(--color-accent)] hover:underline font-medium">
            Sign in
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
          label="Name"
          type="text"
          autoComplete="name"
          value={name}
          onChange={e => setName(e.target.value)}
          optional
          placeholder="How we address you in the app"
        />
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
          autoComplete="new-password"
          minLength={8}
          value={password}
          onChange={e => setPassword(e.target.value)}
          helper="Minimum 8 characters"
        />
        <Button type="submit" variant="primary" fullWidth loading={busy} size="lg">
          {busy ? 'Creating account…' : 'Create account'}
        </Button>
      </form>

      <AuthDivider />
      <GoogleSignInButton href={googleLoginUrl} />
    </AuthLayout>
  )
}
