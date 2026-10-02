import { useNavigate, useLocation, NavLink } from 'react-router-dom'
import { Activity, Bot, ChartNoAxesCombined, CircleDollarSign, CreditCard, LayoutDashboard, ScrollText, Settings2, Users, Workflow } from 'lucide-react'
import { cn } from '../../lib'

const groups = [
  { label: 'Workspace', items: [
    { to: '/admin/overview', label: 'Overview', icon: LayoutDashboard },
    { to: '/admin/audit', label: 'Audit & operations', icon: Activity },
  ] },
  { label: 'People & billing', items: [
    { to: '/admin/users', label: 'Users', icon: Users },
    { to: '/admin/quota', label: 'Credits & billing', icon: CreditCard },
    { to: '/admin/economics', label: 'Economics', icon: CircleDollarSign },
  ] },
  { label: 'AI & automation', items: [
    { to: '/admin/llm-usage', label: 'AI usage', icon: ChartNoAxesCombined },
    { to: '/admin/models', label: 'Models', icon: Bot },
    { to: '/admin/automation', label: 'Automation', icon: Workflow },
  ] },
  { label: 'Configuration', items: [
    { to: '/admin/defaults', label: 'Defaults', icon: Settings2 },
  ] },
] as const

export function AdminNav() {
  const navigate = useNavigate()
  const location = useLocation()
  const current = groups.map(group => group.items.find(item => location.pathname === item.to)?.to).find(Boolean) ?? '/admin/overview'

  return (
    <nav aria-label="Admin sections" className="min-w-0 lg:sticky lg:top-8 lg:self-start">
      <label className="block text-xs font-semibold text-[var(--color-text-muted)] lg:hidden" htmlFor="admin-section">
        Admin section
      </label>
      <select id="admin-section" value={current} onChange={event => navigate(event.target.value)} className="mt-2 h-11 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 text-sm text-[var(--color-text)] lg:hidden">
        {groups.map(group => (
          <optgroup key={group.label} label={group.label}>
            {group.items.map(item => <option key={item.to} value={item.to}>{item.label}</option>)}
          </optgroup>
        ))}
      </select>
      <div className="hidden space-y-5 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-3 lg:block">
        {groups.map(group => (
          <div key={group.label} className="space-y-1">
            <p className="px-3 pb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--color-text-dim)]">{group.label}</p>
            {group.items.map(({ to, label, icon: Icon }) => (
              <NavLink key={to} to={to} className={({ isActive }) => cn(
                'flex min-h-10 items-center gap-2.5 rounded-[var(--radius-md)] px-3 text-sm transition-colors',
                isActive ? 'bg-[var(--color-admin-soft)] font-semibold text-[var(--color-admin)]' : 'text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]',
              )}>
                <Icon size={16} strokeWidth={1.8} aria-hidden="true" />{label}
              </NavLink>
            ))}
          </div>
        ))}
        <div className="flex gap-2 border-t border-[var(--color-border-subtle)] px-3 pt-3 text-xs leading-relaxed text-[var(--color-text-dim)]">
          <ScrollText size={15} className="mt-0.5 shrink-0" aria-hidden="true" /> Changes to settings affect the deployment.
        </div>
      </div>
    </nav>
  )
}
