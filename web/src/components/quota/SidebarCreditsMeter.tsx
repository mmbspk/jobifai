import { Link } from 'react-router-dom'
import { Coins } from 'lucide-react'
import { useQuota } from '../../hooks/useQuota'
import { CreditProgress } from './CreditProgress'

/** Compact credits meter in the sidebar (non-admin, enforced accounts). */
export function SidebarCreditsMeter({ expanded }: { readonly expanded: boolean }) {
  const { data, unlimited, isLoading } = useQuota()

  if (!expanded || isLoading || unlimited || !data) return null

  const isTrial = data.plan === 'trial'
  const total = isTrial
    ? data.allowance_credits || data.trial_remaining_credits || 0
    : data.allowance_credits
  const used = isTrial
    ? Math.max(0, total - (data.trial_remaining_credits ?? 0))
    : data.used_credits

  if (total <= 0 && !data.topup_credits_remaining) return null

  return (
    <Link
      to="/settings/plan"
      className="mx-3 mb-2 block px-3 py-2 rounded-[var(--radius-md)] bg-[var(--color-surface-2)] border border-[var(--color-border)] hover:border-[var(--color-accent)]/35 transition-colors"
    >
      <div className="flex items-center gap-1.5 mb-2">
        <Coins size={11} className="text-[var(--color-accent)]" />
        <span className="text-[10px] font-medium text-[var(--color-text-muted)] uppercase tracking-wide">
          {isTrial ? 'Trial credits' : 'Monthly credits'}
        </span>
      </div>
      <CreditProgress
        label={data.blocked ? 'Limit reached' : 'Used'}
        used={used}
        total={total || 1}
        className="w-full"
      />
      {data.topup_credits_remaining > 0 && (
        <p className="mt-1.5 text-[11px] text-[var(--color-text-dim)] tabular-nums">
          +{data.topup_credits_remaining.toLocaleString()} top-up
        </p>
      )}
    </Link>
  )
}
