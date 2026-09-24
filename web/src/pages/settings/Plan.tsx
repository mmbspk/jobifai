import { useMutation } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Coins, RefreshCw, Sparkles } from 'lucide-react'
import { syncQuotaAfterPayment, useQuota } from '../../hooks/useQuota'
import { billingApi } from '../../api/billing'
import { PageHeader } from '../../components/shell/PageHeader'
import { Button } from '../../components/Button'
import { SettingsSection } from '../../components/settings/settings-ui'
import { StatCard } from '../../components/StatCard'
import { Badge } from '../../components/ui/badge'
import { CreditProgress } from '../../components/quota/CreditProgress'
import { QuotaAlert } from '../../components/quota/QuotaAlert'
import { quotaBlockMessage } from '../../lib/quotaMessages'
import { markPlanReviewed } from '../../lib/setupChecklist'

function fmtCredits(n: number): string {
  return n.toLocaleString()
}

function planBadgeLabel(plan: string): string {
  switch (plan) {
    case 'pro':
      return 'Pro'
    case 'starter':
      return 'Starter'
    case 'trial':
      return 'Trial'
    default:
      return plan
  }
}

export function PlanPage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { data, isLoading, isFetching, refreshQuota } = useQuota()
  const [syncMessage, setSyncMessage] = useState<string | null>(null)
  const paymentSyncStarted = useRef(false)

  useEffect(() => {
    markPlanReviewed()
  }, [])

  useEffect(() => {
    const checkout = searchParams.get('checkout')
    const topup = searchParams.get('topup')
    const isSuccess = checkout === 'success' || topup === 'success'
    if (!isSuccess || paymentSyncStarted.current) return
    paymentSyncStarted.current = true

    setSyncMessage(
      topup === 'success'
        ? 'Top-up received — updating your credit balance…'
        : 'Subscription updated — syncing your plan and credits…',
    )
    navigate('/settings/plan', { replace: true })

    void (async () => {
      await syncQuotaAfterPayment(async () => {
        await refreshQuota()
      })
      setSyncMessage(null)
    })()
  }, [searchParams, navigate, refreshQuota])

  const checkout = useMutation({
    mutationFn: (plan: 'starter' | 'pro') => billingApi.checkout(plan),
    onSuccess: ({ url }) => {
      window.location.href = url
    },
  })

  const topup = useMutation({
    mutationFn: (credits: number) => billingApi.topUp(credits),
    onSuccess: ({ url }) => {
      window.location.href = url
    },
  })

  const portal = useMutation({
    mutationFn: () => billingApi.portal(),
    onSuccess: ({ url }) => {
      window.location.href = url
    },
  })

  const manualRefresh = useMutation({
    mutationFn: () => refreshQuota(),
  })

  if (isLoading || !data) {
    return <p className="text-sm text-[var(--color-text-dim)]">Loading plan…</p>
  }

  if (data.unlimited) {
    return (
      <div className="space-y-6">
        <PageHeader title="Plan & credits" description="Your account has unlimited AI usage." />
      </div>
    )
  }

  const isTrial = data.plan === 'trial'
  const allowance = isTrial
    ? data.allowance_credits || (data.trial_remaining_credits ?? 0)
    : data.allowance_credits
  const used = isTrial
    ? Math.max(0, allowance - (data.trial_remaining_credits ?? 0))
    : data.used_credits

  return (
    <div className="space-y-6">
      <PageHeader
        title="Plan & credits"
        description="Credits cover AI scoring, tailoring, and automation. Monthly allowance renews each billing period; top-ups expire when the period ends."
        actions={
          <Button
            variant="secondary"
            size="sm"
            loading={manualRefresh.isPending || isFetching}
            leftIcon={<RefreshCw size={14} />}
            onClick={() => manualRefresh.mutate()}
          >
            Refresh usage
          </Button>
        }
      />

      {syncMessage && (
        <div className="rounded-[var(--radius-md)] border border-[var(--color-accent)]/30 bg-[var(--color-accent-soft)]/50 px-4 py-3">
          <p className="text-sm text-[var(--color-accent)]" role="status">
            {syncMessage}
          </p>
        </div>
      )}

      {data.blocked && (
        <QuotaAlert message={quotaBlockMessage(data.block_code, data.plan)} />
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <StatCard
          label="Credits remaining"
          value={fmtCredits(data.remaining_credits)}
          sub={isTrial && data.trial_ends_at ? `Trial ends ${new Date(data.trial_ends_at).toLocaleDateString()}` : undefined}
          icon={<Coins size={18} />}
          tone={data.blocked ? 'muted' : 'accent'}
        />
        <StatCard
          label="Current plan"
          value={planBadgeLabel(data.plan)}
          sub={!isTrial && data.period_end ? `Renews ${new Date(data.period_end).toLocaleDateString()}` : 'Subscribe for monthly credits'}
          icon={<Sparkles size={18} />}
          tone="info"
        />
      </div>

      <SettingsSection
        title="Usage this period"
        description={isTrial ? 'Trial ends when you use all credits or reach the time limit — whichever comes first.' : 'Unused monthly credits do not roll over.'}
      >
        <CreditProgress label={isTrial ? 'Trial credits' : 'Monthly credits'} used={used} total={allowance || 1} />
        {data.grace_session_active && (
          <p className="text-sm text-[var(--color-text-muted)]">
            An automation run is using your grace buffer to finish safely.
          </p>
        )}
        {data.topup_credits_remaining > 0 && (
          <p className="text-sm text-[var(--color-text-muted)] tabular-nums">
            Top-up balance: <strong>{fmtCredits(data.topup_credits_remaining)}</strong> credits (expires at period end)
          </p>
        )}
        {!isTrial && data.period_end && (
          <p className="text-xs text-[var(--color-text-dim)]">
            Billing period ends {new Date(data.period_end).toLocaleString()}
          </p>
        )}
        <div className="flex flex-wrap gap-2 pt-1">
          <Badge variant="muted">{Math.min(100, data.usage_percent ?? 0).toFixed(0)}% of allowance used</Badge>
          {data.overage_debt_credits > 0 && (
            <Badge variant="warn">{fmtCredits(data.overage_debt_credits)} debt from prior period</Badge>
          )}
        </div>
      </SettingsSection>

      {data.stripe_configured && (
        <SettingsSection title="Subscribe" description="Monthly billing — cancel anytime before the next period.">
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" disabled={checkout.isPending} onClick={() => checkout.mutate('starter')}>
              Starter
            </Button>
            <Button variant="primary" disabled={checkout.isPending} onClick={() => checkout.mutate('pro')}>
              Pro
            </Button>
            {!isTrial && (
              <Button variant="ghost" disabled={portal.isPending} onClick={() => portal.mutate()}>
                Manage billing
              </Button>
            )}
          </div>
        </SettingsSection>
      )}

      {!isTrial && (data.top_up_packs?.length ?? 0) > 0 && (
        <SettingsSection title="Buy extra credits" description="One-time packs for the current billing period only.">
          <div className="flex flex-wrap gap-2">
            {(data.top_up_packs ?? []).map(p => (
              <Button
                key={p.credits}
                variant="secondary"
                disabled={topup.isPending || !p.stripe_price_id}
                onClick={() => topup.mutate(p.credits)}
              >
                {p.label ?? `${fmtCredits(p.credits)} credits`}
              </Button>
            ))}
          </div>
        </SettingsSection>
      )}
    </div>
  )
}
