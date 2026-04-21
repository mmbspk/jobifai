import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Briefcase, AlertCircle, Star, Settings, Sun, Moon } from 'lucide-react'
import { cn } from '../lib'
import { useQuery } from '@tanstack/react-query'
import { botApi } from '../api/bot'
import { useTheme } from '../hooks/useTheme'

const TABS = [
  { to: '/',                   icon: LayoutDashboard, label: 'Home',        end: true },
  { to: '/jobs/applied',       icon: Briefcase,       label: 'Applied',     end: false },
  { to: '/jobs/cannot-apply',  icon: AlertCircle,     label: 'Manual',      end: false },
  { to: '/review',             icon: Star,            label: 'Review',      badge: true, end: false },
  { to: '/settings/general',   icon: Settings,        label: 'Settings',    end: false },
]

export function BottomNav() {
  const { data: pending } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 30000,
  })
  const pendingCount = pending?.length ?? 0
  const { theme, toggle } = useTheme()

  return (
    <nav className="md:hidden fixed bottom-0 left-0 right-0 z-40 border-t border-[var(--color-border)] bg-[var(--color-surface)]"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      <div className="flex relative">
        {TABS.map(({ to, icon: Icon, label, badge, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                'flex-1 flex flex-col items-center gap-0.5 py-2 text-[0.65rem] relative',
                isActive ? 'text-violet-400' : 'text-[var(--color-text-dim)]',
              )
            }
          >
            {({ isActive }) => (
              <>
                <div className="relative">
                  <Icon size={18} strokeWidth={isActive ? 2.5 : 1.8} />
                  {badge && pendingCount > 0 && (
                    <span className="absolute -top-1 -right-1 text-[9px] bg-violet-500 text-white rounded-full w-3.5 h-3.5 flex items-center justify-center font-bold">
                      {pendingCount > 9 ? '9' : pendingCount}
                    </span>
                  )}
                </div>
                {label}
              </>
            )}
          </NavLink>
        ))}
        <button
          onClick={toggle}
          title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          className="absolute top-1 right-2 p-1 rounded-md text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors"
        >
          {theme === 'dark' ? <Sun size={13} /> : <Moon size={13} />}
        </button>
      </div>
    </nav>
  )
}
