import { NavLink } from 'react-router-dom'
import { cn } from '../../lib'

export function SettingsTabNav({ tabs }: { tabs: readonly { to: string; label: string }[] }) {
  return (
    <div className="overflow-x-auto -mx-1 px-1 pb-1 lg:overflow-visible">
      <div className="flex gap-1 min-w-min lg:flex-col lg:min-w-0">
        {tabs.map(t => (
          <NavLink
            key={t.to}
            to={t.to}
            className={({ isActive }) =>
              cn(
                'min-h-[44px] px-3 py-2 rounded-[var(--radius-md)] text-sm font-medium transition-all text-center whitespace-nowrap flex items-center lg:text-left',
                isActive
                  ? 'bg-[var(--color-accent-soft)] text-[var(--color-accent)]'
                  : 'text-[var(--color-text-muted)] hover:text-[var(--color-text)] hover:bg-[var(--color-surface-2)]',
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
