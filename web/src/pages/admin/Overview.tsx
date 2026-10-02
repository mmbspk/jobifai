import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { Badge } from '../../components/ui/badge'
import { pct, usdFromMicro } from '../../lib/adminFormat'
import { Activity, ArrowUpRight, CircleAlert, CircleCheck, Clock3, Database, Server } from 'lucide-react'

type OverviewResp = {
  system: {
    version: string
    git_sha: string
    migration_version: number
    uptime_seconds: number
    environment: string
    server_time_utc: string
    user_count: number
  }
  ai_today: {
    calls: number
    success_calls: number
    success_rate: number
    raw_cost_usd_micro: number
    loaded_cost_usd_micro: number
    credits_burned: number
    avg_latency_ms: number
  }
  automation: Record<string, number>
  effective_models: {
    task: string
    provider: string
    model: string
    effort: string
    source_label: string
    policy_override: boolean
  }[]
}

type HealthResp = {
  provider: string
  global_model: string
  api_credential_configured: boolean
  proxy_enabled: boolean
  stripe_configured: boolean
  stripe_webhook_configured: boolean
  pricing_catalog_source: string
}

function formatUptime(sec: number): string {
  if (sec <= 0) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

export function AdminOverviewPage() {
  const overviewQ = useQuery({
    queryKey: ['admin-overview'],
    queryFn: () => apiGet<OverviewResp>('/admin/overview'),
  })
  const healthQ = useQuery({
    queryKey: ['admin-health'],
    queryFn: () => apiGet<HealthResp>('/admin/health'),
  })

  const o = overviewQ.data
  const h = healthQ.data
  const sys = o?.system
  const ai = o?.ai_today
  const auto = o?.automation ?? {}
  const issues = [
    h && !h.api_credential_configured && 'AI API key is missing',
    h && !h.stripe_configured && 'Stripe is not configured',
    h && h.stripe_configured && !h.stripe_webhook_configured && 'Stripe webhook is missing',
  ].filter((value): value is string => typeof value === 'string')

  return (
    <div className="space-y-8">
      <PageHeader
        title="Overview"
        description="A quick view of operations, AI activity, and deployment health."
      />

      {overviewQ.isError && (
        <p className="text-sm text-red-600 dark:text-red-400">Failed to load overview.</p>
      )}

      <section aria-label="Key metrics" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Users" value={sys ? sys.user_count.toLocaleString() : '…'} />
        <StatCard label="Applications today" value={String(auto.applications_today ?? '…')} />
        <StatCard label="Pending review" value={String(auto.pending_review_count ?? '…')} />
        <StatCard label="AI cost today · USD" value={ai ? usdFromMicro(ai.loaded_cost_usd_micro) : '…'} />
      </section>

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(280px,1fr)]">
        <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-[var(--shadow-sm)]">
          <div className="flex items-start gap-3">
            <span className="rounded-[var(--radius-md)] bg-[var(--color-admin-soft)] p-2 text-[var(--color-admin)]"><Activity size={19} aria-hidden="true" /></span>
            <div className="min-w-0 flex-1">
              <h2 className="font-semibold">Deployment health</h2>
              <p className="mt-1 text-sm text-[var(--color-text-muted)]">Configuration signals for this environment.</p>
            </div>
            {h && <Badge variant={issues.length ? 'warn' : 'success'}>{issues.length ? `${issues.length} to review` : 'Configured'}</Badge>}
          </div>
          {healthQ.isError && <p className="mt-5 text-sm text-[var(--color-danger)]">Health data is unavailable.</p>}
          {h && <div className="mt-5 grid gap-3 sm:grid-cols-2">
            {[
              ['AI credential', h.api_credential_configured],
              ['Stripe', h.stripe_configured],
              ['Stripe webhook', h.stripe_webhook_configured],
            ].map(([label, ok]) => <div key={String(label)} className="flex items-center gap-2 text-sm">
              {ok ? <CircleCheck size={16} className="text-[var(--color-success)]" aria-hidden="true" /> : <CircleAlert size={16} className="text-[var(--color-warn)]" aria-hidden="true" />}
              <span>{label}</span><span className="ml-auto text-xs text-[var(--color-text-dim)]">{ok ? 'Configured' : 'Not set'}</span>
            </div>)}
            <div className="flex items-center gap-2 text-sm"><Server size={16} className="text-[var(--color-text-muted)]" aria-hidden="true" /><span>Environment</span><span className="ml-auto text-xs text-[var(--color-text-dim)]">{sys?.environment ?? '…'}</span></div>
          </div>}
          {issues.length > 0 && <p className="mt-5 border-t border-[var(--color-border-subtle)] pt-3 text-xs text-[var(--color-text-muted)]">{issues.join(' · ')}</p>}
        </div>
        <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-[var(--shadow-sm)]">
          <div className="flex items-center gap-2"><Clock3 size={18} className="text-[var(--color-admin)]" aria-hidden="true" /><h2 className="font-semibold">Today at a glance</h2></div>
          <dl className="mt-5 divide-y divide-[var(--color-border-subtle)] text-sm">
            {[
              ['Skipped', auto.skipped_today], ['Cannot apply', auto.cannot_apply_today],
              ['Top matches', auto.top_matches_count], ['Active AI users · 15m', auto.recent_llm_users_15min],
            ].map(([label, value]) => <div key={String(label)} className="flex items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0"><dt className="text-[var(--color-text-muted)]">{label}</dt><dd className="font-semibold tabular-nums">{value ?? '…'}</dd></div>)}
          </dl>
        </div>
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div><h2 className="text-base font-semibold">AI activity today</h2><p className="text-sm text-[var(--color-text-muted)]">Usage and cost across all accounts.</p></div>
          <Link to="/admin/llm-usage?period=today" className="inline-flex items-center gap-1 text-sm text-[var(--color-admin)] hover:underline">
            Explore usage <ArrowUpRight size={14} aria-hidden="true" />
          </Link>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard label="LLM calls" value={ai ? String(ai.calls) : '…'} />
          <StatCard label="Success rate" value={ai ? (ai.calls ? pct(ai.success_rate) : 'No calls') : '…'} />
          <StatCard label="Raw cost (USD)" value={ai ? usdFromMicro(ai.raw_cost_usd_micro) : '…'} />
          <StatCard label="Loaded cost (USD)" value={ai ? usdFromMicro(ai.loaded_cost_usd_micro) : '…'} />
        </div>
        <p className="text-xs text-[var(--color-text-dim)]">{ai ? `${ai.credits_burned.toLocaleString()} credits burned · ${ai.calls ? `${Math.round(ai.avg_latency_ms)} ms average latency` : 'No latency data'}` : 'Loading usage details…'}</p>
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div><h2 className="text-base font-semibold">Effective models</h2><p className="text-sm text-[var(--color-text-muted)]">Current task routing and its source.</p></div>
          <Link to="/admin/models" className="text-xs text-[var(--color-admin)] hover:underline">
            Model console →
          </Link>
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[640px]">
            <thead className="sticky top-0 bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-4 py-2 font-medium">Task</th>
                <th className="px-4 py-2 font-medium">Model</th>
                <th className="px-4 py-2 font-medium">Effort</th>
                <th className="px-4 py-2 font-medium">Source</th>
              </tr>
            </thead>
            <tbody>
              {(o?.effective_models ?? []).map(row => (
                <tr key={row.task} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-4 py-2 font-mono text-xs">{row.task}</td>
                  <td className="px-4 py-2">
                    {row.provider}/{row.model}
                  </td>
                  <td className="px-4 py-2">{row.effort || '—'}</td>
                  <td className="px-4 py-2">
                    <Badge variant={row.policy_override ? 'admin' : 'muted'}>{row.source_label}</Badge>
                  </td>
                </tr>
              ))}
              {overviewQ.isSuccess && (o?.effective_models?.length ?? 0) === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-[var(--color-text-dim)]">
                    No model rows.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
      <details className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-5 py-4 text-sm">
        <summary className="flex cursor-pointer items-center gap-2 font-semibold"><Database size={16} className="text-[var(--color-admin)]" aria-hidden="true" /> System details</summary>
        <dl className="mt-4 grid gap-4 border-t border-[var(--color-border-subtle)] pt-4 sm:grid-cols-2 xl:grid-cols-3">
          {[
            ['Version', sys ? `${sys.version}${sys.git_sha ? ` (${sys.git_sha.slice(0, 7)})` : ''}` : '…'],
            ['DB migration', sys?.migration_version ?? '…'], ['Uptime', sys ? formatUptime(sys.uptime_seconds) : '…'],
            ['Server time (UTC)', sys?.server_time_utc?.slice(0, 19) ?? '…'],
            ['Global model', h ? `${h.provider} / ${h.global_model}` : '…'],
            ['Proxy', h ? (h.proxy_enabled ? 'On' : 'Off') : '…'],
            ['Pricing source', h?.pricing_catalog_source ?? '…'],
          ].map(([label, value]) => <div key={String(label)} className="min-w-0"><dt className="text-xs text-[var(--color-text-muted)]">{label}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div>)}
        </dl>
      </details>
    </div>
  )
}
