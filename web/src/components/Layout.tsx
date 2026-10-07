import { useState } from 'react'
import { Sidebar } from './Sidebar'
import { BottomNav } from './BottomNav'
import { Link, Outlet, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { cn } from '../lib'
import { Bell, Moon, Sun } from 'lucide-react'
import { JobifaiLogo } from './brand/JobifaiLogo'
import { useTheme } from '../hooks/useTheme'
import { EmailVerificationBanner } from './auth/EmailVerificationBanner'

export function Layout({ children }: { readonly children?: ReactNode }) {
  const isAdmin = useLocation().pathname.startsWith('/admin')
  const { theme, toggle } = useTheme()
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('sidebar-collapsed') === 'true',
  )

  function toggleCollapsed() {
    setCollapsed(c => {
      const next = !c
      localStorage.setItem('sidebar-collapsed', String(next))
      return next
    })
  }

  return (
    <div className="min-h-dvh bg-[var(--color-background)]">
      <Sidebar collapsed={collapsed} onToggle={toggleCollapsed} />
      <BottomNav />
      <header className="sticky top-0 z-30 grid h-[60px] grid-cols-[2.75rem_1fr_2.75rem] items-center border-b border-[var(--color-border)] bg-[var(--color-background)]/92 px-3 backdrop-blur-xl md:hidden">
        <button type="button" onClick={toggle} aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'} className="flex h-11 w-11 items-center justify-center text-[var(--color-text-muted)]">
          {theme === 'dark' ? <Sun size={18} /> : <Moon size={18} />}
        </button>
        <JobifaiLogo markSize={25} className="justify-self-center text-[1.05rem]" />
        <Link to="/review" aria-label="Open review queue" className="flex h-11 w-11 items-center justify-center text-[var(--color-text-muted)]">
          <Bell size={18} />
        </Link>
      </header>
      <main
        className={cn(
          'max-md:pb-[calc(4rem+env(safe-area-inset-bottom))]',
          collapsed ? 'md:pl-20' : 'md:pl-20 lg:pl-64',
        )}
        style={{ transition: 'padding-left 200ms' }}
      >
        <div className={cn('mx-auto px-4 py-6 md:px-7 md:py-10 lg:px-8 lg:py-12', isAdmin ? 'max-w-[1600px]' : 'max-w-[1120px]')}>
          <EmailVerificationBanner />
          {children ?? <Outlet />}
        </div>
      </main>
    </div>
  )
}
