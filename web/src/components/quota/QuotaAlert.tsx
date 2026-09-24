import { Link } from 'react-router-dom'
import { cn } from '../../lib'

type QuotaAlertProps = {
  readonly title?: string
  readonly message: string
  readonly className?: string
  readonly compact?: boolean
  readonly showPlanLink?: boolean
}

/** Inline alert for exhausted credits — matches BotStatusCard error panel styling. */
export function QuotaAlert({
  title = 'AI credits used up',
  message,
  className,
  compact = false,
  showPlanLink = true,
}: QuotaAlertProps) {
  if (compact) {
    return (
      <p className={cn('text-xs text-[var(--color-text-muted)]', className)}>
        {message}
        {showPlanLink && (
          <>
            {' '}
            <Link to="/settings/plan" className="font-medium text-[var(--color-accent)] hover:underline">
              Plan & credits
            </Link>
          </>
        )}
      </p>
    )
  }

  return (
    <div
      className={cn(
        'rounded-[var(--radius-md)] border border-[var(--color-danger)]/30 bg-[var(--color-danger-soft)] px-4 py-3',
        className,
      )}
      role="alert"
    >
      <p className="text-sm font-medium text-[var(--color-danger)]">{title}</p>
      <p className="text-sm text-[var(--color-text-muted)] mt-1">{message}</p>
      {showPlanLink && (
        <Link
          to="/settings/plan"
          className="inline-block mt-2 text-sm font-medium text-[var(--color-accent)] hover:underline"
        >
          View plan & buy credits →
        </Link>
      )}
    </div>
  )
}
