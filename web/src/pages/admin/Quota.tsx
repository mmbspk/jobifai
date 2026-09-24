import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { quotaApi } from '../../api/quota'
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

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Badge variant="admin">Admin</Badge>
        <PageHeader
          title="Credits & billing"
          description="Deployment-wide credit formula, trial limits, Starter/Pro grants, and Stripe price IDs."
        />
      </div>

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
