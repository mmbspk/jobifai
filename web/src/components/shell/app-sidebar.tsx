import { useEffect, useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, ClipboardCheck, Send, CircleSlash2, Hand, Star, Sparkles, Settings,
  Sun, Moon, LogOut, Zap, ChevronLeft, ChevronRight, Shield,
} from 'lucide-react'
import * as Tooltip from '@radix-ui/react-tooltip'
import { cn } from '../../lib'
import { useQuery } from '@tanstack/react-query'
import { botApi } from '../../api/bot'
import { usageApi } from '../../api/usage'
import { useTheme } from '../../hooks/useTheme'
import { useAuth } from '../../contexts/AuthContext'
import { JobifaiLogo } from '../brand/JobifaiLogo'
import { SidebarCreditsMeter } from '../quota/SidebarCreditsMeter'

const PRIMARY_NAV = [
  { to: '/', icon: LayoutDashboard, label: 'Home', end: true },
  { to: '/review', icon: ClipboardCheck, label: 'Review', badge: true },
  { to: '/jobs/applied', icon: Send, label: 'Applied' },
  { to: '/jobs/top-matches', icon: Star, label: 'Top Matches' },
  { to: '/generate', icon: Sparkles, label: 'Generate' },
] as const

const JOBS_NAV = [
  { to: '/jobs/skipped', icon: CircleSlash2, label: 'Skipped' },
  { to: '/jobs/cannot-apply', icon: Hand, label: 'Cannot Apply' },
] as const

interface AppSidebarProps {
  readonly collapsed: boolean
  readonly onToggle: () => void
}

function NavItem({
  to, icon: Icon, label, badge, end, collapsed, pendingCount,
}: {
  to: string
  icon: typeof LayoutDashboard
  label: string
  badge?: boolean
  end?: boolean
  collapsed: boolean
  pendingCount: number
}) {
  const link = (
    <NavLink
      to={to}
      end={end}
      title={collapsed ? label : undefined}
      className={({ isActive }) =>
        cn(
          'flex min-h-[43px] items-center rounded-[var(--radius-md)] text-sm transition-colors',
          collapsed ? 'justify-center px-0' : 'gap-3 px-3',
          isActive
            ? 'bg-[var(--color-accent-soft)] text-[var(--color-accent)] font-semibold'
            : 'text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]',
        )
      }
    >
      {({ isActive }) => (
        <>
          <Icon size={16} strokeWidth={1.8} className={isActive ? 'text-[var(--color-accent)]' : ''} />
          {!collapsed && <span className="flex-1 truncate">{label}</span>}
          {!collapsed && badge && pendingCount > 0 && (
            <span className="text-[10px] bg-[var(--color-accent)] text-white rounded-full min-w-[1.125rem] h-[1.125rem] px-1 flex items-center justify-center font-semibold">
              {pendingCount > 9 ? '9+' : pendingCount}
            </span>
          )}
          {collapsed && badge && pendingCount > 0 && (
            <span className="absolute top-0.5 right-0.5 w-2 h-2 rounded-full bg-[var(--color-accent)]" />
          )}
        </>
      )}
    </NavLink>
  )

  if (!collapsed) return <div className="relative">{link}</div>

  return (
    <Tooltip.Root delayDuration={0}>
      <Tooltip.Trigger asChild>
        <div className="relative">{link}</div>
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content
          side="right"
          sideOffset={8}
          className="z-50 px-2 py-1 text-xs rounded-[var(--radius-sm)] bg-[var(--color-surface-3)] text-[var(--color-text)] border border-[var(--color-border)] shadow-[var(--shadow-md)]"
        >
          {label}
          {badge && pendingCount > 0 ? ` (${pendingCount})` : ''}
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  )
}

function useMinWidthLg() {
  const [lg, setLg] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia('(min-width: 1024px)').matches,
  )
  useEffect(() => {
    const mq = window.matchMedia('(min-width: 1024px)')
    const onChange = () => setLg(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return lg
}

export function AppSidebar({ collapsed, onToggle }: AppSidebarProps) {
  const isLg = useMinWidthLg()
  const expanded = isLg && !collapsed
  const { user, logout } = useAuth()
  const isAdmin = user?.is_admin === true
  const { data: pending } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 30000,
  })
  const { data: usage } = useQuery({
    queryKey: ['usage-session'],
    queryFn: usageApi.session,
    refetchInterval: 15000,
    enabled: isAdmin,
  })
  const pendingCount = pending?.length ?? 0
  const { theme, toggle } = useTheme()
  const navigate = useNavigate()

  return (
    <Tooltip.Provider>
      <aside
        className={cn(
          'hidden md:flex flex-col fixed left-0 top-0 bottom-0 z-40',
          'border-r border-[var(--color-border-subtle)] bg-[var(--color-surface)]/96 backdrop-blur-xl',
          expanded ? 'md:w-20 lg:w-64' : 'md:w-20',
        )}
        style={{ transition: 'width 200ms' }}
      >
        <div className="flex h-[76px] items-center px-5 gap-1">
          <NavLink to="/" className="flex items-center flex-1 min-w-0 overflow-hidden">
            <JobifaiLogo markSize={28} showWordmark={expanded} className="text-sm" />
          </NavLink>
          <button
            type="button"
            onClick={onToggle}
            title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            className="hidden lg:flex p-1 rounded-md hover:bg-[var(--color-surface-2)] text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors shrink-0"
          >
            {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
          </button>
        </div>

        <nav className="flex-1 px-3 py-2 overflow-y-auto space-y-5">
          <div className="space-y-0.5">
            {PRIMARY_NAV.map(item => (
              <NavItem
                key={item.to}
                {...item}
                collapsed={!expanded}
                pendingCount={pendingCount}
              />
            ))}
            <NavItem
              to="/settings/application"
              icon={Settings}
              label="Settings"
              collapsed={!expanded}
              pendingCount={0}
            />
          </div>

          {expanded && (
            <div>
              <p className="px-3 mb-1.5 text-[0.65rem] font-semibold uppercase tracking-widest text-[var(--color-text-dim)]">
                Jobs
              </p>
              <div className="space-y-0.5">
                {JOBS_NAV.map(item => (
                  <NavItem key={item.to} {...item} collapsed={false} pendingCount={0} />
                ))}
              </div>
            </div>
          )}

          {!expanded && (
            <div className="space-y-0.5 pt-1 border-t border-[var(--color-border-subtle)]">
              {JOBS_NAV.map(item => (
                <NavItem key={item.to} {...item} collapsed pendingCount={0} />
              ))}
            </div>
          )}

          {isAdmin && (
            <div>
              {expanded && (
                <p className="px-3 mb-1.5 text-[0.65rem] font-semibold uppercase tracking-widest text-[var(--color-admin)]">
                  Admin
                </p>
              )}
              <NavLink
                to="/admin"
                title={!expanded ? 'Admin' : undefined}
                className={({ isActive }) =>
                  cn(
                    'flex items-center py-2 rounded-[var(--radius-md)] text-sm transition-colors',
                    !expanded ? 'justify-center' : 'gap-2.5 pl-2.5 pr-3',
                    isActive
                      ? 'bg-[var(--color-admin-soft)] text-[var(--color-admin)] font-medium'
                      : 'text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)]',
                  )
                }
              >
                <Shield size={16} strokeWidth={1.8} />
                {expanded && <span>Admin</span>}
              </NavLink>
            </div>
          )}
        </nav>

        <SidebarCreditsMeter expanded={expanded} />

        {isAdmin && expanded && usage && (usage.input_tokens > 0 || usage.output_tokens > 0) ? (
          <div className="mx-3 mb-2 px-3 py-2 rounded-[var(--radius-md)] bg-[var(--color-surface-2)] border border-[var(--color-border)]">
            <div className="flex items-center gap-1.5 mb-1.5">
              <Zap size={11} className="text-[var(--color-accent)]" />
              <span className="text-[10px] font-medium text-[var(--color-text-muted)] uppercase tracking-wide">Session usage</span>
            </div>
            <div className="flex justify-between text-[11px] text-[var(--color-text-muted)] tabular-nums">
              <span title="Input tokens">↑ {usage.input_tokens.toLocaleString()}</span>
              <span title="Output tokens">↓ {usage.output_tokens.toLocaleString()}</span>
            </div>
            {usage.estimated_cost_usd != null && (
              <div className="mt-1 text-[11px] text-[var(--color-text-dim)] tabular-nums">
                ~${usage.estimated_cost_usd.toFixed(4)}
              </div>
            )}
          </div>
        ) : null}

        <div className="px-3 py-3 border-t border-[var(--color-border)] space-y-2">
          {expanded && user && (
            <div className="flex items-center gap-2 min-w-0">
              <div className="w-7 h-7 rounded-full bg-[var(--color-accent-soft)] flex items-center justify-center text-[var(--color-accent)] text-xs font-bold shrink-0">
                {user.display_name?.[0]?.toUpperCase() ?? user.email[0].toUpperCase()}
              </div>
              <span className="text-xs text-[var(--color-text-muted)] truncate flex-1">{user.display_name || user.email}</span>
            </div>
          )}
          <div className={cn('flex items-center', !expanded ? 'flex-col gap-2' : 'justify-between')}>
            <button
              type="button"
              onClick={toggle}
              title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
              className="p-2 rounded-md hover:bg-[var(--color-surface-2)] text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors min-w-[44px] min-h-[44px] lg:min-h-0 lg:min-w-0 lg:p-1 flex items-center justify-center"
            >
              {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
            </button>
            {user && (
              <button
                type="button"
                onClick={async () => { await logout(); navigate('/login', { replace: true }) }}
                title="Sign out"
                className="p-2 rounded-md hover:bg-[var(--color-surface-2)] text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors min-w-[44px] min-h-[44px] lg:min-h-0 lg:min-w-0 lg:p-1 flex items-center justify-center"
              >
                <LogOut size={16} />
              </button>
            )}
          </div>
        </div>
      </aside>
    </Tooltip.Provider>
  )
}
