import { ArrowRight } from 'lucide-react'
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
  readonly tone?: 'accent' | 'success' | 'info' | 'muted'
}

const toneClasses = {
  accent: 'bg-[var(--color-accent-soft)] text-[var(--color-accent)]',
  success: 'bg-[var(--color-success-soft)] text-[var(--color-success)]',
  info: 'bg-[var(--color-info-soft)] text-[var(--color-info)]',
  muted: 'bg-[var(--color-surface-2)] text-[var(--color-text-muted)]',
}

export function StatCard({
  label, value, sub, icon, accent, className, onClick, tone = 'accent',
}: Props) {
  const inner = (
    <>
      {icon && (
        <span className={cn('row-span-2 flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)]', toneClasses[tone])}>
          {icon}
        </span>
      )}
      <div className="text-[var(--color-text-muted)] text-xs font-medium">
        {label}
      </div>
      <div className="text-2xl font-bold leading-none text-[var(--color-text)] tabular-nums">{value}</div>
      {sub && <div className={cn('text-xs text-[var(--color-text-dim)]', icon && 'col-start-2')}>{sub}</div>}
      {onClick && <ArrowRight size={16} className="absolute right-4 top-1/2 -translate-y-1/2 text-[var(--color-text-dim)] transition-transform group-hover:translate-x-0.5 group-hover:text-[var(--color-accent)]" />}
    </>
  )

  const classes = cn(
    'group relative rounded-[var(--radius-lg)] border p-4 pr-10 grid gap-x-3 gap-y-1.5',
    icon ? 'grid-cols-[2.5rem_1fr]' : 'grid-cols-1',
    'bg-[var(--color-surface)] border-[var(--color-border)]',
    'shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-card)] transition-all',
    accent && 'border-[var(--color-accent)]/40 bg-[var(--color-accent-soft)]/30',
    className,
  )

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        className={cn(classes, 'cursor-pointer hover:border-[var(--color-accent)]/40 text-left w-full')}
      >
        {inner}
      </button>
    )
  }

  return <div className={classes}>{inner}</div>
}
