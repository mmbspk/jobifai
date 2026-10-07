import { useState } from 'react'
import { Link } from 'react-router-dom'
import { userApi } from '../api/user'
import { Button } from '../components/Button'
import { Input } from '../components/ui/input'
import { AuthLayout } from '../components/auth/AuthLayout'

export function ForgotPassword() {
  const [email, setEmail] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await userApi.forgotPassword(email.trim().toLowerCase())
      setSubmitted(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed — please try again')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Reset your password"
      subtitle="Enter your email and we'll send you a reset link."
      footer={
        <Link to="/login" className="text-[var(--color-accent)] hover:underline font-medium">
          Back to sign in
        </Link>
      }
    >
      {submitted ? (
        <div className="rounded-[var(--radius-md)] border border-green-200 bg-green-50 px-4 py-4 text-sm text-green-800 dark:border-green-800/40 dark:bg-green-950/40 dark:text-green-300">
          Request received — if an account with that address exists, you'll receive a reset link shortly.
          Check your inbox and spam folder.
        </div>
      ) : (
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
          <Button type="submit" variant="primary" fullWidth loading={busy} size="lg">
            {busy ? 'Sending…' : 'Send reset link'}
          </Button>
        </form>
      )}
    </AuthLayout>
  )
}
