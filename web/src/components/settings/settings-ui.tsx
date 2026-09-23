import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '../../lib'

export function SettingsSection({ title, description, children }: {
  title: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden shadow-[var(--shadow-sm)]">
      <div className="px-4 py-3 border-b border-[var(--color-border-subtle)]">
        <h2 className="text-sm font-semibold text-[var(--color-text)]">{title}</h2>
        {description && <p className="text-xs text-[var(--color-text-dim)] mt-1">{description}</p>}
      </div>
      <div className="p-4 space-y-4">{children}</div>
    </section>
  )
}

export function SettingsField({ label, sub, children, layout = 'row' }: {
  label: string
  sub?: string
  children: React.ReactNode
  layout?: 'row' | 'column'
}) {
  if (layout === 'column') {
    return (
      <div className="space-y-2">
        <div>
          <div className="text-sm text-[var(--color-text)]">{label}</div>
          {sub && <div className="text-xs text-[var(--color-text-dim)] mt-0.5">{sub}</div>}
        </div>
        {children}
      </div>
    )
  }
  return (
    <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
      <div className="min-w-0">
        <div className="text-sm text-[var(--color-text)]">{label}</div>
        {sub && <div className="text-xs text-[var(--color-text-dim)] mt-0.5">{sub}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

export function SettingsCollapsibleSection({
  title,
  children,
  defaultOpen = false,
  badge,
}: {
  title: string
  children: React.ReactNode
  defaultOpen?: boolean
  badge?: string
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden shadow-[var(--shadow-sm)]">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center justify-between gap-2 px-4 py-3 text-sm font-medium text-[var(--color-text)] hover:bg-[var(--color-surface-2)] transition-colors"
      >
        <span className="flex items-center gap-2">
          {title}
          {badge && (
            <span className="text-[0.65rem] font-medium px-1.5 py-0.5 rounded-full bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
              {badge}
            </span>
          )}
        </span>
        <ChevronDown size={14} className={cn('text-[var(--color-text-dim)] transition-transform shrink-0', open && 'rotate-180')} />
      </button>
      {open && <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)]">{children}</div>}
    </section>
  )
}

export function SettingsSelect(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(
        'h-10 min-w-[140px] bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-[var(--radius-md)] px-3 text-sm text-[var(--color-text)]',
        'focus:outline-none focus:border-[var(--color-accent)] focus:ring-2 focus:ring-[var(--color-accent)]/20',
        props.className,
      )}
    />
  )
}

export function SettingsNumberInput({ value, onChange, min, max, className }: {
  value: number
  onChange: (v: number) => void
  min?: number
  max?: number
  className?: string
}) {
  return (
    <input
      type="number"
      value={value}
      min={min}
      max={max}
      onChange={e => onChange(Number(e.target.value))}
      className={cn(
        'w-24 h-10 bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-[var(--radius-md)] px-3 text-sm tabular-nums',
        'focus:outline-none focus:border-[var(--color-accent)] focus:ring-2 focus:ring-[var(--color-accent)]/20',
        className,
      )}
    />
  )
}
