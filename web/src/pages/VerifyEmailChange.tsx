import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { userApi } from '../api/user'
import { AuthLayout } from '../components/auth/AuthLayout'

type Status = 'loading' | 'success' | 'expired' | 'invalid' | 'error'

export function VerifyEmailChangePage() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''
  const [status, setStatus] = useState<Status>('loading')
  const [message, setMessage] = useState('')

  useEffect(() => {
    if (!token) { setStatus('invalid'); return }
    userApi.verifyEmailChange(token)
      .then(res => { setMessage(res.message); setStatus('success') })
      .catch((e: unknown) => {
        const err = e as { message?: string; code?: string }
        const code = err.code ?? ''
        if (code === 'expired') setStatus('expired')
        else if (code === 'invalid' || code === 'not_found') setStatus('invalid')
        else { setMessage(err.message ?? ''); setStatus('error') }
      })
  }, [token])

  const body = (() => {
    switch (status) {
      case 'loading':
        return <p className="text-[var(--color-text-muted)]">Verifying your new email address…</p>
      case 'success':
        return (
          <>
            <p className="font-medium text-green-600 dark:text-green-400">
              {message || 'Your email address has been updated successfully.'}
            </p>
            <p className="mt-2 text-sm text-[var(--color-text-muted)]">
              You have been signed out of all other sessions. Please log in again with your new email address.
            </p>
          </>
        )
      case 'expired':
        return (
          <p>
            This link has expired — email change links are valid for 24 hours.
            Sign in and request a new change from your account settings.
          </p>
        )
      case 'invalid':
        return <p>This link is invalid or has already been used.</p>
      default:
        return <p className="text-red-600 dark:text-red-400">{message || 'Verification failed — please try again.'}</p>
    }
  })()

  const footer = (
    <Link to="/login" className="underline underline-offset-2 hover:no-underline">
      {status === 'success' ? 'Continue to login →' : 'Go to login →'}
    </Link>
  )

  return (
    <AuthLayout
      title="Confirm email change"
      subtitle={status === 'loading' ? 'Please wait…' : ''}
      footer={footer}
    >
      <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 text-sm">
        {body}
      </div>
    </AuthLayout>
  )
}
