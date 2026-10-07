import { useState } from 'react'
import { useAuth } from '../../contexts/AuthContext'
import { userApi } from '../../api/user'
import { Button } from '../../components/Button'
import { Input } from '../../components/ui/input'
import { PageHeader } from '../../components/shell/PageHeader'

export function SecuritySettingsPage() {
  const { user } = useAuth()

  const [current, setCurrent] = useState('')
  const [newPass, setNewPass] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSuccess(false)
    if (newPass !== confirm) {
      setError('New passwords do not match')
      return
    }
    if (newPass.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    setBusy(true)
    try {
      await userApi.changePassword(current, newPass)
      setSuccess(true)
      setCurrent('')
      setNewPass('')
      setConfirm('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Password change failed — please try again')
    } finally {
      setBusy(false)
    }
  }

  const isGoogleOnly = user?.has_google && !user?.has_password

  return (
    <div className="space-y-6">
      <PageHeader title="Security" description="Manage your account password." />

      {isGoogleOnly ? (
        <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 text-sm text-[var(--color-text-muted)]">
          <p>This account uses Google sign-in and does not currently have a password. Continue signing in with Google.</p>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="space-y-4 max-w-sm">
          {success && (
            <p className="text-sm text-green-700 bg-green-50 border border-green-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-green-300 dark:bg-green-950/40 dark:border-green-800/40" role="status">
              Password updated successfully.
            </p>
          )}
          {error && (
            <p className="text-sm text-[var(--color-danger)] bg-[var(--color-danger-soft)] border border-[var(--color-danger)]/20 rounded-[var(--radius-md)] px-3 py-2" role="alert">
              {error}
            </p>
          )}
          <Input
            label="Current password"
            type="password"
            required
            autoComplete="current-password"
            value={current}
            onChange={e => setCurrent(e.target.value)}
          />
          <Input
            label="New password"
            type="password"
            required
            autoComplete="new-password"
            minLength={8}
            value={newPass}
            onChange={e => setNewPass(e.target.value)}
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
          <Button type="submit" variant="primary" loading={busy}>
            {busy ? 'Saving…' : 'Update password'}
          </Button>
        </form>
      )}
    </div>
  )
}
