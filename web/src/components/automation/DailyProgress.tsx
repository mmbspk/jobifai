import { cn } from '../../lib'

interface DailyProgressProps {
  readonly count: number
  readonly limit: number
  readonly className?: string
}

export function DailyProgress({ count, limit, className }: DailyProgressProps) {
  if (limit <= 0) return null
  const pct = Math.min((count / limit) * 100, 100)

  return (
    <div className={cn('space-y-1.5 w-full sm:w-48', className)}>
      <div className="flex justify-between text-xs text-[var(--color-text-muted)]">
        <span>Daily applications</span>
        <span className="tabular-nums">{count} / {limit}</span>
      </div>
      <div className="h-2 rounded-full bg-[var(--color-surface-2)] overflow-hidden">
        <div
          className="h-full rounded-full bg-[var(--color-accent)] transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}
