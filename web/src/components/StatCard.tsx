import { cn } from '../lib'
import type { ReactNode } from 'react'

interface Props {
  readonly label: string
  readonly value: string | number
  readonly sub?: string
  readonly icon?: ReactNode
  readonly accent?: boolean
  readonly className?: string
  readonly onClick?: () => void
}

export function StatCard({ label, value, sub, icon, accent, className, onClick }: Props) {
  const inner = (
    <>
      <div className="flex items-center gap-2 text-[var(--color-text-muted)] text-xs font-medium uppercase tracking-wide">
        {icon && <span className="text-[var(--color-accent)]">{icon}</span>}
        {label}
      </div>
      <div className="text-3xl font-bold text-[var(--color-text)] tabular-nums">{value}</div>
      {sub && <div className="text-xs text-[var(--color-text-dim)]">{sub}</div>}
    </>
  )

  const classes = cn(
    'rounded-xl border p-4 flex flex-col gap-1',
    'bg-[var(--color-surface)] border-[var(--color-border)]',
    'shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)] transition-shadow',
    accent && 'border-[var(--color-accent)] shadow-[0_0_20px_var(--color-accent-glow)]',
    className,
  )

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        className={cn(classes, 'cursor-pointer hover:border-violet-500/40 text-left w-full')}
      >
        {inner}
      </button>
    )
  }

  return <div className={classes}>{inner}</div>
}
