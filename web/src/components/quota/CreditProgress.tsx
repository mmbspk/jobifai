import { cn } from '../../lib'

interface CreditProgressProps {
  readonly label: string
  readonly used: number
  readonly total: number
  readonly className?: string
  readonly warnFromPercent?: number
}

/** Credit usage bar — matches DailyProgress styling. */
export function CreditProgress({
  label,
  used,
  total,
  className,
  warnFromPercent = 85,
}: CreditProgressProps) {
  if (total <= 0) return null
  const pct = Math.min((used / total) * 100, 100)
  const barTone =
    pct >= 100 ? 'bg-[var(--color-danger)]' : pct >= warnFromPercent ? 'bg-[var(--color-warn)]' : 'bg-[var(--color-accent)]'

  return (
    <div className={cn('space-y-1.5 w-full', className)}>
      <div className="flex justify-between text-xs text-[var(--color-text-muted)]">
        <span>{label}</span>
        <span className="tabular-nums">
          {used.toLocaleString()} / {total.toLocaleString()}
        </span>
      </div>
      <div className="h-2 rounded-full bg-[var(--color-surface-2)] overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-300', barTone)}
          style={{ width: `${pct}%` }}
          role="progressbar"
          aria-valuenow={Math.round(pct)}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>
    </div>
  )
}
