import { Check } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { PublicPlan } from '../../api/plans'
import { cn } from '../../lib'

const featureCopy = [
  'AI role matching and scoring',
  'Tailored application materials',
  'Review before submission',
  'Application tracking',
  'Supported job platforms',
]

function formatPrice(plan: PublicPlan): string {
  if (plan.id === 'trial') return '$0'
  if (plan.unit_amount == null || !plan.currency) return 'Price at checkout'

  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: plan.currency,
      maximumFractionDigits: plan.unit_amount % 100 === 0 ? 0 : 2,
    }).format(plan.unit_amount / 100)
  } catch {
    return `${plan.currency} ${(plan.unit_amount / 100).toFixed(2)}`
  }
}

export function PlanCard({
  plan,
  recommended = false,
  compact = false,
}: Readonly<{
  plan: PublicPlan
  recommended?: boolean
  compact?: boolean
}>) {
  const isTrial = plan.id === 'trial'
  const price = formatPrice(plan)

  return (
    <article
      className={cn(
        'relative flex h-full flex-col rounded-[var(--radius-xl)] border bg-[var(--color-surface)]',
        compact ? 'p-5' : 'p-6 sm:p-7',
        recommended
          ? 'border-[var(--color-accent)]/45 shadow-[var(--shadow-card)]'
          : 'border-[var(--color-border)]',
      )}
    >
      {recommended && (
        <span className="absolute right-5 top-5 rounded-full bg-[var(--color-accent-soft)] px-2.5 py-1 text-[11px] font-semibold text-[var(--color-accent)]">
          Recommended
        </span>
      )}

      <div className="pr-24">
        <p className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--color-text-dim)]">{plan.name}</p>
        <div className="mt-3 flex items-end gap-1.5">
          <span className="text-3xl font-bold tracking-[-0.04em] text-[var(--color-text)]">{price}</span>
          {!isTrial && plan.unit_amount != null && (
            <span className="pb-1 text-sm text-[var(--color-text-dim)]">/{plan.interval || 'month'}</span>
          )}
        </div>
        <p className="mt-2 text-sm text-[var(--color-text-muted)]">
          {isTrial
            ? `${plan.trial_days || 7} days to explore Jobifai`
            : `${plan.credits.toLocaleString()} AI credits each month`}
        </p>
      </div>

      {isTrial && (
        <div className="mt-5 rounded-[var(--radius-md)] bg-[var(--color-surface-2)] px-3 py-2 text-sm text-[var(--color-text-muted)]">
          {plan.credits.toLocaleString()} trial credits included.
        </div>
      )}

      {!compact && (
        <ul className="mt-6 space-y-3">
          {featureCopy.map(feature => (
            <li key={feature} className="flex gap-2.5 text-sm text-[var(--color-text-muted)]">
              <Check size={16} className="mt-0.5 shrink-0 text-[var(--color-accent)]" />
              <span>{feature}</span>
            </li>
          ))}
        </ul>
      )}

      <div className="mt-auto pt-7">
        <Link
          to="/register"
          className={cn(
            'inline-flex min-h-[44px] w-full items-center justify-center rounded-[var(--radius-md)] px-4 text-sm font-semibold transition-colors',
            recommended
              ? 'bg-[var(--color-accent)] text-white hover:bg-[var(--color-accent-hover)]'
              : 'border border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text)] hover:bg-[var(--color-surface-2)]',
          )}
        >
          {isTrial ? 'Start free' : `Choose ${plan.name}`}
        </Link>
      </div>
    </article>
  )
}
