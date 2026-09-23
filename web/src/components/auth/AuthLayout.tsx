import { Link } from 'react-router-dom'
import { JobifaiLogo } from '../brand/JobifaiLogo'

interface AuthLayoutProps {
  readonly title: string
  readonly subtitle: string
  readonly children: React.ReactNode
  readonly footer: React.ReactNode
}

export function AuthLayout({ title, subtitle, children, footer }: AuthLayoutProps) {
  return (
    <div className="min-h-dvh auth-bg flex items-center justify-center px-4 py-10">
      <div className="w-full max-w-[400px] space-y-8">
        <div className="flex flex-col items-center text-center space-y-3">
          <Link to="/login" className="inline-flex rounded-[var(--radius-md)] focus-visible:outline-offset-4">
            <JobifaiLogo markSize={32} className="text-base" />
          </Link>
          <div>
            <h1 className="text-2xl font-bold text-[var(--color-text)]">{title}</h1>
            <p className="text-sm text-[var(--color-text-dim)] mt-1.5">{subtitle}</p>
          </div>
        </div>
        {children}
        <div className="text-center text-sm text-[var(--color-text-dim)]">{footer}</div>
      </div>
    </div>
  )
}

export function GoogleSignInButton({ href }: { readonly href: string }) {
  return (
    <a
      href={href}
      className={[
        'flex items-center justify-center gap-2 w-full min-h-[44px] py-2.5',
        'rounded-[var(--radius-md)] border border-[var(--color-border)]',
        'text-sm font-medium text-[var(--color-text)]',
        'bg-[var(--color-surface)] hover:bg-[var(--color-surface-2)] transition-colors',
      ].join(' ')}
    >
      <GoogleIcon />
      Continue with Google
    </a>
  )
}

export function AuthDivider() {
  return (
    <div className="relative flex items-center gap-3">
      <div className="flex-1 h-px bg-[var(--color-border)]" />
      <span className="text-xs text-[var(--color-text-dim)]">or</span>
      <div className="flex-1 h-px bg-[var(--color-border)]" />
    </div>
  )
}

function GoogleIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
      <path fill="#4285F4" d="M17.64 9.2c0-.637-.057-1.251-.164-1.84H9v3.481h4.844c-.209 1.125-.843 2.078-1.796 2.716v2.259h2.908c1.702-1.567 2.684-3.875 2.684-6.615z"/>
      <path fill="#34A853" d="M9 18c2.43 0 4.467-.806 5.956-2.184l-2.908-2.259c-.806.54-1.837.86-3.048.86-2.344 0-4.328-1.584-5.036-3.711H.957v2.332A8.997 8.997 0 0 0 9 18z"/>
      <path fill="#FBBC05" d="M3.964 10.706A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.706V4.962H.957A8.996 8.996 0 0 0 0 9c0 1.452.348 2.827.957 4.038l3.007-2.332z"/>
      <path fill="#EA4335" d="M9 3.58c1.321 0 2.508.454 3.44 1.345l2.582-2.58C13.463.891 11.426 0 9 0A8.997 8.997 0 0 0 .957 4.962L3.964 7.294C4.672 5.163 6.656 3.58 9 3.58z"/>
    </svg>
  )
}
