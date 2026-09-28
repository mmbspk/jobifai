import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { JobifaiLogo } from '../brand/JobifaiLogo'
import { cn } from '../../lib'

export function PublicLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <div className="min-h-dvh bg-[var(--color-background)] text-[var(--color-text)]">
      <header className="sticky top-0 z-40 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_oklch,var(--color-background)_88%,transparent)] backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-[1180px] items-center gap-6 px-4 sm:px-6">
          <Link to="/welcome" aria-label="Jobifai home" className="shrink-0">
            <JobifaiLogo markSize={28} />
          </Link>

          <nav className="hidden items-center gap-1 md:flex" aria-label="Public navigation">
            <a href="/welcome#how-it-works" className="rounded-lg px-3 py-2 text-sm text-[var(--color-text-muted)] hover:text-[var(--color-text)]">
              How it works
            </a>
            <a href="/welcome#features" className="rounded-lg px-3 py-2 text-sm text-[var(--color-text-muted)] hover:text-[var(--color-text)]">
              Features
            </a>
            <NavLink
              to="/pricing"
              className={({ isActive }) => cn(
                'rounded-lg px-3 py-2 text-sm',
                isActive ? 'text-[var(--color-text)]' : 'text-[var(--color-text-muted)] hover:text-[var(--color-text)]',
              )}
            >
              Pricing
            </NavLink>
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <Link
              to="/login"
              className="inline-flex min-h-[44px] items-center justify-center rounded-[var(--radius-md)] px-3 text-sm font-medium text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]"
            >
              Sign in
            </Link>
            <Link
              to="/register"
              className="inline-flex min-h-[44px] items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent)] px-4 text-sm font-semibold text-white shadow-[var(--shadow-sm)] transition-colors hover:bg-[var(--color-accent-hover)]"
            >
              Start free
            </Link>
          </div>
        </div>
      </header>

      <main>{children}</main>

      <footer className="border-t border-[var(--color-border-subtle)]">
        <div className="mx-auto grid max-w-[1180px] gap-8 px-4 py-10 sm:px-6 md:grid-cols-[1fr_auto] md:items-end">
          <div className="space-y-3">
            <JobifaiLogo />
            <p className="max-w-md text-sm text-[var(--color-text-muted)]">
              Job application automation with clear review points, transparent usage, and you in control.
            </p>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2 text-sm text-[var(--color-text-muted)]">
            <Link to="/pricing" className="hover:text-[var(--color-text)]">Pricing</Link>
            <Link to="/login" className="hover:text-[var(--color-text)]">Sign in</Link>
            <Link to="/register" className="hover:text-[var(--color-text)]">Create account</Link>
          </div>
        </div>
      </footer>
    </div>
  )
}
