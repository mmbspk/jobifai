import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { apiGet } from '../../api/client'
import { quotaApi } from '../../api/quota'
import { StatCard } from '../../components/StatCard'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection } from '../../components/settings/settings-ui'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
import { Badge } from '../../components/ui/badge'
import type { QuotaDefaults, TopUpPack } from '../../types'

const DEFAULT_PACKS: TopUpPack[] = [
  { credits: 1000, label: '1,000 credits', stripe_price_id: '' },
  { credits: 2500, label: '2,500 credits', stripe_price_id: '' },
  { credits: 5000, label: '5,000 credits', stripe_price_id: '' },
]

const INITIAL: QuotaDefaults = {
  enforcement_default: true,
  credits_per_usd: 1000,
  service_markup: 0.5,
  per_call_fee_usd: 0.002,
  trial_credits: 500,
  trial_days: 7,
  starter_credits_monthly: 3000,
  pro_credits_monthly: 8000,
  subscriber_grace_credits: 200,
  top_up_packs: DEFAULT_PACKS,
}

export function AdminQuotaPage() {
  const qc = useQueryClient()
  const { data: loaded } = useQuery({ queryKey: ['admin-quota-defaults'], queryFn: quotaApi.adminDefaults.get })
  const { data: summary } = useQuery({
    queryKey: ['admin-quota-summary'],
    queryFn: () =>
      apiGet<{ users_by_plan: Record<string, number>; total_credits_burned: number }>('/admin/quota/summary'),
  })
  const [def, setDef] = useState<QuotaDefaults>(INITIAL)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (loaded) {
      setDef({
        ...INITIAL,
        ...loaded,
        top_up_packs: loaded.top_up_packs?.length ? loaded.top_up_packs : DEFAULT_PACKS,
      })
    }
  }, [loaded])

  const save = useMutation({
    mutationFn: () => quotaApi.adminDefaults.set(def),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-quota-defaults'] })
      qc.invalidateQueries({ queryKey: ['quota-status'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  const setPack = (i: number, patch: Partial<TopUpPack>) => {
    const packs = [...(def.top_up_packs ?? DEFAULT_PACKS)]
    packs[i] = { ...packs[i], ...patch }
    setDef({ ...def, top_up_packs: packs })
  }

  const packs = def.top_up_packs?.length ? def.top_up_packs : DEFAULT_PACKS

  const [eventStatus, setEventStatus] = useState('')
  const [eventUser, setEventUser] = useState('')
  const { data: billingSummary } = useQuery({
    queryKey: ['admin-billing-summary'],
    queryFn: adminApi.billing.summary,
  })
  const { data: billingUsers = [] } = useQuery({
    queryKey: ['admin-billing-users'],
    queryFn: adminApi.billing.users,
  })
  const { data: webhookEvents = [], refetch: refetchEvents } = useQuery({
    queryKey: ['admin-billing-events', eventStatus, eventUser],
    queryFn: () =>
      adminApi.billing.webhookEvents({
        status: eventStatus || undefined,
        user_id: eventUser || undefined,
        limit: 50,
      }),
  })
  const reconcile = useMutation({
    mutationFn: (userId: string) => adminApi.billing.reconcileUser(userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-billing-users'] })
      void refetchEvents()
    },
  })

  return (
    <div className="space-y-6">
      <div>
        <PageHeader
          title="Credits & billing"
          description="Deployment-wide credit formula, trial limits, Starter/Pro grants, and Stripe price IDs."
        />
      </div>

      {summary && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Object.entries(summary.users_by_plan ?? {}).map(([plan, n]) => (
            <StatCard key={plan} label={`Users (${plan || 'unknown'})`} value={String(n)} />
          ))}
          <StatCard label="Total credits burned (all time)" value={summary.total_credits_burned.toLocaleString()} />
        </div>
      )}

      <SettingsSection
        title="Billing operations"
        description="Stripe configuration as seen by the server (env vars). Reconcile repairs missed webhooks."
      >
        <div className="flex flex-wrap gap-2">
          <Badge variant={billingSummary?.config.stripe_configured ? 'accent' : 'warn'}>
            Stripe API {billingSummary?.config.stripe_configured ? 'configured' : 'missing key'}
          </Badge>
          <Badge variant={billingSummary?.config.webhook_configured ? 'accent' : 'warn'}>
            Webhook secret {billingSummary?.config.webhook_configured ? 'set' : 'missing'}
          </Badge>
          {billingSummary?.config.insecure_webhook_allowed && (
            <Badge variant="warn">Insecure webhook bypass enabled</Badge>
          )}
        </div>
        {billingSummary?.plan_counts?.length ? (
          <p className="text-sm text-[var(--color-text-muted)]">
            Plans:{' '}
            {billingSummary.plan_counts.map(p => `${p.plan} (${p.count})`).join(' · ')}
          </p>
        ) : null}
        <div className="overflow-x-auto rounded-[var(--radius-md)] border border-[var(--color-border)]">
          <table className="w-full text-left text-xs">
            <thead className="bg-[var(--color-surface-elevated)] text-[var(--color-text-dim)]">
              <tr>
                <th className="px-3 py-2 font-medium">User</th>
                <th className="px-3 py-2 font-medium">Plan</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Credits</th>
                <th className="px-3 py-2 font-medium">Customer</th>
                <th className="px-3 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {billingUsers.map(u => (
                <tr key={u.user_id} className="border-t border-[var(--color-border)]">
                  <td className="px-3 py-2">{u.email}</td>
                  <td className="px-3 py-2">{u.plan}</td>
                  <td className="px-3 py-2">{u.stripe_subscription_status || '—'}</td>
                  <td className="px-3 py-2 tabular-nums">
                    {u.period_used_credits}/{u.allowance_credits}
                    {u.topup_credits_remaining > 0 ? ` +${u.topup_credits_remaining} top-up` : ''}
                  </td>
                  <td className="px-3 py-2 font-mono text-[10px]">{u.stripe_customer_id || '—'}</td>
                  <td className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={!u.stripe_customer_id || reconcile.isPending}
                      onClick={() => {
                        if (window.confirm(`Reconcile Stripe state for ${u.email}?`)) {
                          reconcile.mutate(u.user_id)
                        }
                      }}
                    >
                      Reconcile
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex flex-wrap gap-2 pt-2">
          <select
            className={inputClassName}
            value={eventStatus}
            onChange={e => setEventStatus(e.target.value)}
            aria-label="Webhook event status filter"
          >
            <option value="">All statuses</option>
            <option value="processed">processed</option>
            <option value="failed">failed</option>
            <option value="ignored">ignored</option>
            <option value="received">received</option>
          </select>
          <input
            className={inputClassName}
            placeholder="Filter by user id"
            value={eventUser}
            onChange={e => setEventUser(e.target.value)}
          />
        </div>
        <div className="max-h-64 overflow-auto rounded-[var(--radius-md)] border border-[var(--color-border)]">
          <table className="w-full text-left text-xs">
            <thead className="sticky top-0 bg-[var(--color-surface-elevated)] text-[var(--color-text-dim)]">
              <tr>
                <th className="px-3 py-2 font-medium">Received</th>
                <th className="px-3 py-2 font-medium">Type</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Attempts</th>
                <th className="px-3 py-2 font-medium">User</th>
                <th className="px-3 py-2 font-medium">Error</th>
              </tr>
            </thead>
            <tbody>
              {webhookEvents.map(ev => (
                <tr key={ev.event_id} className="border-t border-[var(--color-border)]">
                  <td className="px-3 py-2 whitespace-nowrap">{new Date(ev.received_at).toLocaleString()}</td>
                  <td className="px-3 py-2">{ev.event_type}</td>
                  <td className="px-3 py-2">{ev.status}</td>
                  <td className="px-3 py-2">{ev.attempt_count}</td>
                  <td className="px-3 py-2 font-mono text-[10px]">{ev.user_id || ev.stripe_customer_id || '—'}</td>
                  <td className="px-3 py-2 text-[var(--color-text-dim)]">{ev.error_code || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </SettingsSection>

      <SettingsSection title="Credit formula" description="Applied after each LLM call when converting token cost to credits.">
        <SettingsField label="Credits per USD" sub="1000 = 1000 credits per $1 of loaded burn">
          <SettingsNumberInput
            value={def.credits_per_usd}
            min={1}
            onChange={v => setDef({ ...def, credits_per_usd: v })}
            className="w-28"
          />
        </SettingsField>
        <SettingsField label="Service markup" sub="Added on LLM cost (0.5 = +50%)">
          <input
            type="number"
            step="0.01"
            min={0}
            className={inputClassName}
            value={def.service_markup}
            onChange={e => setDef({ ...def, service_markup: Number(e.target.value) })}
          />
        </SettingsField>
        <SettingsField label="Per-call fee (USD)" sub="Flat fee on every LLM call">
          <input
            type="number"
            step="0.001"
            min={0}
            className={inputClassName}
            value={def.per_call_fee_usd}
            onChange={e => setDef({ ...def, per_call_fee_usd: Number(e.target.value) })}
          />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Trial" description="New accounts receive trial credits and a time limit — whichever runs out first blocks AI.">
        <SettingsField label="Trial credits">
          <SettingsNumberInput value={def.trial_credits} min={0} onChange={v => setDef({ ...def, trial_credits: v })} />
        </SettingsField>
        <SettingsField label="Trial days">
          <SettingsNumberInput value={def.trial_days} min={1} onChange={v => setDef({ ...def, trial_days: v })} />
        </SettingsField>
        <Switch
          label="Enforce credits for new users"
          helper="When off, new users are not limited until an admin enables enforcement per account."
          checked={def.enforcement_default}
          onCheckedChange={v => setDef({ ...def, enforcement_default: v })}
        />
      </SettingsSection>

      <SettingsSection title="Plans" description="Monthly credit grants applied when Stripe renews a subscription.">
        <SettingsField label="Starter credits / month">
          <SettingsNumberInput
            value={def.starter_credits_monthly}
            min={0}
            onChange={v => setDef({ ...def, starter_credits_monthly: v })}
          />
        </SettingsField>
        <SettingsField label="Pro credits / month">
          <SettingsNumberInput
            value={def.pro_credits_monthly}
            min={0}
            onChange={v => setDef({ ...def, pro_credits_monthly: v })}
          />
        </SettingsField>
        <SettingsField label="Subscriber grace credits" sub="One automation session may exceed allowance by up to this amount">
          <SettingsNumberInput
            value={def.subscriber_grace_credits}
            min={0}
            onChange={v => setDef({ ...def, subscriber_grace_credits: v })}
          />
        </SettingsField>
        <SettingsField label="Stripe price — Starter" layout="column">
          <input
            className={inputClassName}
            value={def.stripe_price_starter ?? ''}
            onChange={e => setDef({ ...def, stripe_price_starter: e.target.value })}
            placeholder="price_…"
          />
        </SettingsField>
        <SettingsField label="Stripe price — Pro" layout="column">
          <input
            className={inputClassName}
            value={def.stripe_price_pro ?? ''}
            onChange={e => setDef({ ...def, stripe_price_pro: e.target.value })}
            placeholder="price_…"
          />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Top-up packs" description="One-time Stripe prices; purchased credits expire at the subscriber’s current period end.">
        {packs.map((p, i) => (
          <SettingsField key={p.credits} label={`${p.label ?? p.credits} — Stripe price ID`} layout="column">
            <input
              className={inputClassName}
              value={p.stripe_price_id ?? ''}
              onChange={e => setPack(i, { stripe_price_id: e.target.value })}
              placeholder="price_…"
            />
          </SettingsField>
        ))}
      </SettingsSection>

      <Button variant="primary" fullWidth loading={save.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => save.mutate()}>
        {saved ? 'Saved' : 'Save quota defaults'}
      </Button>
    </div>
  )
}
