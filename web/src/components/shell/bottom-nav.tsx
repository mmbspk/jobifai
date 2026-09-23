import { useState } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { LayoutDashboard, Briefcase, ClipboardCheck, Sparkles, Settings, Sun, Moon } from 'lucide-react'
import { cn } from '../../lib'
import { useQuery } from '@tanstack/react-query'
import { botApi } from '../../api/bot'
import { useTheme } from '../../hooks/useTheme'
import { DialogRoot, DialogTrigger, DialogContent } from '../ui/dialog'

const TABS = [
  { to: '/', icon: LayoutDashboard, label: 'Home', end: true },
  { key: 'jobs', icon: Briefcase, label: 'Jobs' },
  { to: '/review', icon: ClipboardCheck, label: 'Review', badge: true },
  { to: '/generate', icon: Sparkles, label: 'Generate' },
  { to: '/settings/application', icon: Settings, label: 'Settings' },
] as const

const JOB_LINKS = [
  { to: '/jobs/applied', label: 'Applied' },
  { to: '/jobs/top-matches', label: 'Top Matches' },
  { to: '/jobs/skipped', label: 'Skipped' },
  { to: '/jobs/cannot-apply', label: 'Cannot Apply' },
] as const

export function AppBottomNav() {
  const [jobsOpen, setJobsOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const { data: pending } = useQuery({
    queryKey: ['review-pending'],
    queryFn: botApi.reviewPending,
    refetchInterval: 30000,
  })
  const pendingCount = pending?.length ?? 0
  const { theme, toggle } = useTheme()

  const jobsActive = JOB_LINKS.some(j => location.pathname.startsWith(j.to))

  return (
    <nav
      className="fixed bottom-0 left-0 right-0 z-40 border-t border-[var(--color-border)] bg-[var(--color-surface)]/95 backdrop-blur-sm md:hidden"
      style={{ paddingBottom: 'max(10px, env(safe-area-inset-bottom))' }}
    >
      <div className="flex relative max-w-lg mx-auto">
        {TABS.map(tab => {
          if ('key' in tab && tab.key === 'jobs') {
            return (
              <DialogRoot key="jobs" open={jobsOpen} onOpenChange={setJobsOpen}>
                <DialogTrigger asChild>
                  <button
                    type="button"
                    className={cn(
                      'flex-1 flex flex-col items-center justify-center gap-0.5 py-2 min-h-[52px] text-[0.65rem] border-t-2 transition-colors',
                      jobsActive
                        ? 'text-[var(--color-accent)] border-[var(--color-accent)]'
                        : 'text-[var(--color-text-dim)] border-transparent',
                    )}
                  >
                    <Briefcase size={20} strokeWidth={jobsActive ? 2.2 : 1.8} />
                    Jobs
                  </button>
                </DialogTrigger>
                <DialogContent title="Jobs" description="Application history and matches">
                  <ul className="space-y-1">
                    {JOB_LINKS.map(link => (
                      <li key={link.to}>
                        <button
                          type="button"
                          className="w-full text-left px-3 py-3 rounded-[var(--radius-md)] text-sm font-medium text-[var(--color-text)] hover:bg-[var(--color-surface-2)]"
                          onClick={() => {
                            setJobsOpen(false)
                            navigate(link.to)
                          }}
                        >
                          {link.label}
                        </button>
                      </li>
                    ))}
                  </ul>
                </DialogContent>
              </DialogRoot>
            )
          }

          const { to, icon: Icon, label, end, badge } = tab as {
            to: string
            icon: typeof LayoutDashboard
            label: string
            end?: boolean
            badge?: boolean
          }

          return (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'flex-1 flex flex-col items-center justify-center gap-0.5 py-2 min-h-[52px] text-[0.65rem] relative border-t-2 transition-colors',
                  isActive
                    ? 'text-[var(--color-accent)] border-[var(--color-accent)]'
                    : 'text-[var(--color-text-dim)] border-transparent',
                )
              }
            >
              {({ isActive }) => (
                <>
                  <div className="relative">
                    <Icon size={20} strokeWidth={isActive ? 2.2 : 1.8} />
                    {badge && pendingCount > 0 && (
                      <span className="absolute -top-1 -right-2 text-[9px] bg-[var(--color-accent)] text-white rounded-full min-w-[14px] h-[14px] px-0.5 flex items-center justify-center font-bold">
                        {pendingCount > 9 ? '9' : pendingCount}
                      </span>
                    )}
                  </div>
                  {label}
                </>
              )}
            </NavLink>
          )
        })}
        <button
          type="button"
          onClick={toggle}
          title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          className="absolute top-1 right-1 p-2 rounded-md text-[var(--color-text-dim)] hover:text-[var(--color-text)] min-w-[44px] min-h-[44px] flex items-center justify-center"
        >
          {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
        </button>
      </div>
    </nav>
  )
}
