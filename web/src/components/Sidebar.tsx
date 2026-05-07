import { NavLink, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Briefcase, SkipForward, AlertCircle, TrendingUp, Star, FileText, Settings, Sun, Moon, LogOut, Zap,
} from 'lucide-react'
import { cn } from '../lib'
import { useQuery } from '@tanstack/react-query'
import { botApi } from '../api/bot'
import { usageApi } from '../api/usage'
import { useTheme } from '../hooks/useTheme'
import { useAuth } from '../contexts/AuthContext'

const NAV = [
  { to: '/',                    icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/jobs/applied',        icon: Briefcase,       label: 'Applied' },
  { to: '/jobs/skipped',        icon: SkipForward,     label: 'Skipped' },
  { to: '/jobs/cannot-apply',   icon: AlertCircle,     label: 'Cannot Apply' },
  { to: '/jobs/top-matches',    icon: TrendingUp,      label: 'Top Matches' },
  { to: '/review',              icon: Star,            label: 'Review', badge: true },
  { to: '/generate',            icon: FileText,        label: 'Generate' },
  { to: '/settings/general',    icon: Settings,        label: 'Settings' },
]

export function Sidebar() {
  const { data: pending } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 30000,
  })
  const { data: usage } = useQuery({
    queryKey: ['usage-session'],
    queryFn: usageApi.session,
    refetchInterval: 15000,
  })
  const pendingCount = pending?.length ?? 0
  const { theme, toggle } = useTheme()
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  return (
    <aside className="hidden md:flex flex-col fixed left-0 top-0 bottom-0 w-56 border-r border-[var(--color-border)] bg-[var(--color-surface)] z-40">
      {/* Brand */}
      <div className="px-4 py-4 border-b border-[var(--color-border)]">
        <div className="flex items-center gap-2">
          <div className="w-7 h-7 rounded-lg bg-gradient-to-br from-violet-500 to-violet-700 flex items-center justify-center text-white text-xs font-bold">J</div>
          <span className="font-semibold text-sm text-[var(--color-text)]">Jobifai</span>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex-1 px-2 py-3 overflow-y-auto">
        <div className="px-3 mb-2">
          <span className="text-[0.65rem] font-semibold uppercase tracking-widest text-[var(--color-text-dim)]">Navigation</span>
        </div>
        <div className="space-y-0.5">
        {NAV.map(({ to, icon: Icon, label, badge }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-2.5 pl-2.5 pr-3 py-2 rounded-lg text-sm transition-colors',
                isActive
                  ? 'bg-[var(--color-surface-2)] text-[var(--color-text)] font-medium border-l-2 border-[var(--color-accent)]'
                  : 'text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)] border-l-2 border-transparent',
              )
            }
          >
            {({ isActive }) => (
              <>
                <Icon size={15} className={isActive ? 'text-[var(--color-accent)]' : ''} />
                <span className="flex-1">{label}</span>
                {badge && pendingCount > 0 && (
                  <span className="text-xs bg-violet-500 text-white rounded-full w-4 h-4 flex items-center justify-center font-medium">
                    {pendingCount > 9 ? '9+' : pendingCount}
                  </span>
                )}
              </>
            )}
          </NavLink>
        ))}
        </div>
      </nav>

      {/* Usage */}
      {usage && (usage.input_tokens > 0 || usage.output_tokens > 0) ? (
        <div className="mx-3 mb-2 px-3 py-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)]">
          <div className="flex items-center gap-1.5 mb-1.5">
            <Zap size={11} className="text-violet-400" />
            <span className="text-[10px] font-medium text-[var(--color-text-muted)] uppercase tracking-wide">Tokens</span>
          </div>
          <div className="flex justify-between text-[11px] text-[var(--color-text-muted)]">
            <span title="Input tokens">↑ {usage.input_tokens.toLocaleString()}</span>
            <span title="Output tokens">↓ {usage.output_tokens.toLocaleString()}</span>
          </div>
          {usage.estimated_cost_usd != null && (
            <div className="mt-1 text-[11px] text-[var(--color-text-dim)]">
              ~${usage.estimated_cost_usd.toFixed(4)}
            </div>
          )}
        </div>
      ) : null}

      {/* Footer */}
      <div className="px-3 py-3 border-t border-[var(--color-border)] space-y-2">
        {user && (
          <div className="flex items-center gap-2 min-w-0">
            <div className="w-6 h-6 rounded-full bg-[var(--color-accent)] flex items-center justify-center text-white text-xs font-bold shrink-0">
              {user.display_name?.[0]?.toUpperCase() ?? user.email[0].toUpperCase()}
            </div>
            <span className="text-xs text-[var(--color-text-muted)] truncate flex-1">{user.display_name || user.email}</span>
          </div>
        )}
        <div className="flex items-center justify-between">
          <button
            onClick={toggle}
            title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
            className="p-1 rounded-md hover:bg-[var(--color-surface-2)] text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors"
          >
            {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
          </button>
          {user && (
            <button
              onClick={async () => { await logout(); navigate('/login', { replace: true }) }}
              title="Sign out"
              className="p-1 rounded-md hover:bg-[var(--color-surface-2)] text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors"
            >
              <LogOut size={14} />
            </button>
          )}
        </div>
      </div>
    </aside>
  )
}
