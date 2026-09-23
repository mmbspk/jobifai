import { NavLink } from 'react-router-dom'
import { cn } from '../../lib'

export function SettingsTabNav({ tabs }: { tabs: readonly { to: string; label: string }[] }) {
  return (
    <div className="overflow-x-auto -mx-1 px-1 pb-1">
      <div className="flex gap-1 p-1 min-w-min bg-[var(--color-surface)] rounded-[var(--radius-lg)] border border-[var(--color-border)]">
        {tabs.map(t => (
          <NavLink
            key={t.to}
            to={t.to}
            className={({ isActive }) =>
              cn(
                'py-2 px-3 rounded-[var(--radius-md)] text-sm font-medium transition-all text-center whitespace-nowrap min-h-[44px] sm:min-h-0 flex items-center',
                isActive
                  ? 'bg-[var(--color-accent-soft)] text-[var(--color-accent)] border border-[var(--color-accent)]/30'
                  : 'text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] border border-transparent',
              )
            }
          >
            {t.label}
          </NavLink>
        ))}
      </div>
    </div>
  )
}
