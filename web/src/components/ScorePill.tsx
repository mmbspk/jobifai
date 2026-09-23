import { cn, formatScore, scoreBand, scoreBandLabel } from '../lib'

interface Props {
  score: number
  /** Show band label (e.g. Strong) beside the number. */
  showLabel?: boolean
  compact?: boolean
  className?: string
}

const BAND_CLASS = {
  low: 'bg-[var(--color-danger-soft)] text-[var(--color-danger)] border-[var(--color-danger)]/20',
  moderate: 'bg-[var(--color-warn-soft)] text-[var(--color-warn)] border-[var(--color-warn)]/20',
  strong: 'bg-[var(--color-accent-soft)] text-[var(--color-accent)] border-[var(--color-accent)]/25',
  excellent: 'bg-[var(--color-success-soft)] text-[var(--color-success)] border-[var(--color-success)]/25',
} as const

export function ScorePill({ score, showLabel = false, compact = false, className }: Props) {
  const band = scoreBand(score)
  const label = scoreBandLabel(band)
  const display = formatScore(score)
  const title = `${label} match · score ${display} / 10`

  if (compact) {
    return (
      <span
        title={title}
        className={cn(
          'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-semibold tabular-nums',
          BAND_CLASS[band],
          className,
        )}
      >
        {display}
      </span>
    )
  }

  return (
    <span
      title={title}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold tabular-nums',
        BAND_CLASS[band],
        className,
      )}
    >
      <span>{display}</span>
      {showLabel && <span className="font-medium opacity-90">{label}</span>}
    </span>
  )
}
