import { useState } from 'react'
import { useAuth } from '../../contexts/AuthContext'
import { MailWarning } from 'lucide-react'

export function EmailVerificationBanner() {
  const { user, resendVerification } = useAuth()
  const [sent, setSent] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Only show for email/password accounts that are unverified.
  // Google OAuth accounts are always pre-verified; has_google accounts skip this.
  if (!user || user.email_verified || user.has_google) return null

  async function handleResend() {
    setBusy(true)
    setError(null)
    try {
      await resendVerification()
      setSent(true)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Could not send — please try again later')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-lg border border-amber-400/40 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-950/40 dark:text-amber-200"
    >
      <MailWarning size={16} className="mt-0.5 shrink-0 text-amber-500" />
      <div className="flex-1">
        <span>Please verify your email address — check your inbox for a confirmation link.</span>
        {error && <p className="mt-1 text-xs text-red-600 dark:text-red-400">{error}</p>}
      </div>
      {!sent ? (
        <button
          type="button"
          onClick={handleResend}
          disabled={busy}
          className="shrink-0 text-xs font-medium underline underline-offset-2 hover:no-underline disabled:opacity-50"
        >
          {busy ? 'Sending…' : 'Resend email'}
        </button>
      ) : (
        <span className="shrink-0 text-xs font-medium text-amber-700 dark:text-amber-300">Sent!</span>
      )}
    </div>
  )
}
