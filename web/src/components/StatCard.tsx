import { cn } from '../lib'
import type { ReactNode } from 'react'

interface Props {
  label: string
  value: string | number
  sub?: string
  icon?: ReactNode
  accent?: boolean
  className?: string
}

export function StatCard({ label, value, sub, icon, accent, className }: Props) {
  return (
    <div
      className={cn(
        'rounded-xl border p-4 flex flex-col gap-1',
        'bg-[var(--color-surface)] border-[var(--color-border)]',
        accent && 'border-[var(--color-accent)] shadow-[0_0_20px_var(--color-accent-glow)]',
        className,
      )}
    >
      <div className="flex items-center gap-2 text-[var(--color-text-muted)] text-sm">
        {icon && <span className="text-[var(--color-accent)]">{icon}</span>}
        {label}
      </div>
      <div className="text-3xl font-bold text-[var(--color-text)] tabular-nums">{value}</div>
      {sub && <div className="text-xs text-[var(--color-text-dim)]">{sub}</div>}
    </div>
  )
}
