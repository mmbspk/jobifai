import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { userApi } from '../api/user'
import { Button } from '../components/Button'
import { Input } from '../components/ui/input'
import { AuthLayout } from '../components/auth/AuthLayout'

export function ResetPassword() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''
  const navigate = useNavigate()

  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  if (!token) {
    return (
      <AuthLayout
        title="Invalid link"
        subtitle=""
        footer={<Link to="/forgot-password" className="text-[var(--color-accent)] hover:underline font-medium">Request a new link</Link>}
      >
        <p className="text-sm text-[var(--color-text-muted)]">
          This password reset link is missing or invalid. Please request a new one.
        </p>
      </AuthLayout>
    )
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (password !== confirm) {
      setError('Passwords do not match')
      return
    }
    setBusy(true)
    try {
      await userApi.resetPassword(token, password)
      navigate('/login', { state: { resetSuccess: true } })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Reset failed — please try again')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Set a new password"
      subtitle="Choose a strong password for your account."
      footer={
        <Link to="/login" className="text-[var(--color-accent)] hover:underline font-medium">
          Back to sign in
        </Link>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {error && (
          <p className="text-sm text-[var(--color-danger)] bg-[var(--color-danger-soft)] border border-[var(--color-danger)]/20 rounded-[var(--radius-md)] px-3 py-2" role="alert">
            {error}
          </p>
        )}
        <Input
          label="New password"
          type="password"
          required
          autoComplete="new-password"
          minLength={8}
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
        <Input
          label="Confirm new password"
          type="password"
          required
          autoComplete="new-password"
          minLength={8}
          value={confirm}
          onChange={e => setConfirm(e.target.value)}
        />
        <Button type="submit" variant="primary" fullWidth loading={busy} size="lg">
          {busy ? 'Saving…' : 'Set new password'}
        </Button>
      </form>
    </AuthLayout>
  )
}
