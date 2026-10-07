import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { userApi } from '../api/user'
import { AuthLayout } from '../components/auth/AuthLayout'

type Status = 'loading' | 'success' | 'already-used' | 'expired' | 'invalid' | 'error'

export function VerifyEmailPage() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''
  const [status, setStatus] = useState<Status>('loading')
  const [message, setMessage] = useState('')

  useEffect(() => {
    if (!token) { setStatus('invalid'); return }
    userApi.verifyEmail(token)
      .then(res => { setMessage(res.message); setStatus('success') })
      .catch((e: Error) => {
        const msg = e.message ?? ''
        if (msg.includes('already been used')) setStatus('already-used')
        else if (msg.includes('expired')) setStatus('expired')
        else if (msg.includes('invalid')) setStatus('invalid')
        else { setMessage(msg); setStatus('error') }
      })
  }, [token])

  const body = (() => {
    switch (status) {
      case 'loading':
        return <p className="text-[var(--color-text-muted)]">Verifying your email address…</p>
      case 'success':
        return (
          <p className="font-medium text-green-600 dark:text-green-400">
            {message || 'Your email has been verified. You can now use all features.'}
          </p>
        )
      case 'already-used':
        return <p>This link has already been used. Your email may already be verified.</p>
      case 'expired':
        return <p>This link has expired (links are valid for 24 hours). Log in to request a new one.</p>
      case 'invalid':
        return <p>The verification link is invalid or incomplete.</p>
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
      title="Email Verification"
      subtitle={status === 'loading' ? 'Please wait…' : ''}
      footer={footer}
    >
      <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 text-sm">
        {body}
      </div>
    </AuthLayout>
  )
}
