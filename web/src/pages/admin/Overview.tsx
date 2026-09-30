import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { Badge } from '../../components/ui/badge'
import { pct, usdFromMicro } from '../../lib/adminFormat'

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

  return (
    <div className="space-y-6">
      <PageHeader
        title="Overview"
        description="Operational snapshot — system health, AI spend today, automation, and effective models."
      />

      {overviewQ.isError && (
        <p className="text-sm text-red-600 dark:text-red-400">Failed to load overview.</p>
      )}

      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">System</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            label="Version"
            value={sys ? `${sys.version}${sys.git_sha ? ` (${sys.git_sha.slice(0, 7)})` : ''}` : '…'}
          />
          <StatCard label="DB migration" value={sys ? String(sys.migration_version) : '…'} />
          <StatCard label="Uptime" value={sys ? formatUptime(sys.uptime_seconds) : '…'} />
          <StatCard label="Environment" value={sys?.environment ?? '…'} />
          <StatCard label="Users" value={sys ? String(sys.user_count) : '…'} />
          <StatCard label="Server time (UTC)" value={sys?.server_time_utc?.slice(0, 19) ?? '…'} />
        </div>
        {h && (
          <div className="flex flex-wrap gap-2 text-xs">
            <Badge variant={h.api_credential_configured ? 'success' : 'warn'}>
              API key {h.api_credential_configured ? 'configured' : 'missing'}
            </Badge>
            <Badge variant="muted">{h.provider} · global {h.global_model}</Badge>
            {h.proxy_enabled && <Badge variant="muted">Proxy on</Badge>}
            <Badge variant={h.stripe_configured ? 'success' : 'muted'}>
              Stripe {h.stripe_configured ? 'ok' : 'not set'}
            </Badge>
            <Badge variant={h.stripe_webhook_configured ? 'success' : 'muted'}>
              Webhook {h.stripe_webhook_configured ? 'ok' : 'not set'}
            </Badge>
            <Badge variant="muted">Pricing: {h.pricing_catalog_source}</Badge>
          </div>
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">AI today</h2>
          <Link to="/admin/llm-usage?period=today" className="text-xs text-[var(--color-admin)] hover:underline">
            Open usage explorer →
          </Link>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard label="LLM calls" value={ai ? String(ai.calls) : '…'} />
          <StatCard label="Success rate" value={ai ? pct(ai.success_rate) : '…'} />
          <StatCard label="Raw cost (USD)" value={ai ? usdFromMicro(ai.raw_cost_usd_micro) : '…'} />
          <StatCard label="Loaded cost (USD)" value={ai ? usdFromMicro(ai.loaded_cost_usd_micro) : '…'} />
          <StatCard label="Credits burned" value={ai ? ai.credits_burned.toLocaleString() : '…'} />
          <StatCard label="Avg latency" value={ai ? `${Math.round(ai.avg_latency_ms)} ms` : '…'} />
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Automation</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard label="Applications today" value={String(auto.applications_today ?? '…')} />
          <StatCard label="Skipped today" value={String(auto.skipped_today ?? '…')} />
          <StatCard label="Cannot apply today" value={String(auto.cannot_apply_today ?? '…')} />
          <StatCard label="Pending review" value={String(auto.pending_review_count ?? '…')} />
          <StatCard label="Top matches" value={String(auto.top_matches_count ?? '…')} />
          <StatCard label="Active LLM users (15m)" value={String(auto.recent_llm_users_15min ?? '…')} />
        </div>
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Effective models</h2>
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
    </div>
  )
}
