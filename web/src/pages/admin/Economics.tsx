import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { ADMIN_PERIODS, type AdminPeriod, pct, usdFromMicro } from '../../lib/adminFormat'
import type { EconomicsOverview } from '../../types'

type TaskRow = {
  task: string
  calls: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  credits_burned: number
  avg_latency_ms: number
  error_rate?: number
  error_rate_available: boolean
}

export function AdminEconomicsPage() {
  const [period, setPeriod] = useState<AdminPeriod>('30d')

  const overviewQ = useQuery({
    queryKey: ['admin-economics-overview', period],
    queryFn: () => apiGet<EconomicsOverview>(`/admin/economics/overview?period=${period}`),
  })
  const tasksQ = useQuery({
    queryKey: ['admin-economics-tasks', period],
    queryFn: () => apiGet<{ tasks: TaskRow[]; failure_tracking_enabled: boolean }>(`/admin/economics/tasks?period=${period}`),
  })
  const modelsQ = useQuery({
    queryKey: ['admin-economics-models', period],
    queryFn: () => apiGet<{ models: Record<string, unknown>[] }>(`/admin/economics/models?period=${period}`),
  })
  const usersQ = useQuery({
    queryKey: ['admin-economics-users', period],
    queryFn: () => apiGet<{ users: Record<string, unknown>[] }>(`/admin/economics/users?period=${period}`),
  })

  const o = overviewQ.data
  const errRate = o && o.calls > 0 ? 1 - o.success_calls / o.calls : null
  const res = o?.resource
  const ai = o?.ai

  return (
    <div className="space-y-6">
      <PageHeader
        title="Economics"
        description="Billing-grade usage from the event ledger — AI and resource charges combined (no prompt or resume content)."
      />

      <div className="flex flex-wrap gap-2">
        {ADMIN_PERIODS.map(p => (
          <button
            key={p.value}
            type="button"
            onClick={() => setPeriod(p.value)}
            className={
              period === p.value
                ? 'rounded-[var(--radius-md)] border border-[var(--color-admin)]/40 bg-[var(--color-admin-soft)] px-3 py-1.5 text-sm'
                : 'rounded-[var(--radius-md)] border border-[var(--color-border)] px-3 py-1.5 text-sm text-[var(--color-text-muted)]'
            }
          >
            {p.label}
          </button>
        ))}
      </div>

      {overviewQ.isError && (
        <p className="text-sm text-[var(--color-danger)]">Could not load economics overview.</p>
      )}

      <section className="space-y-2">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Totals (AI + resource)</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <StatCard label="Total credits burned" value={o ? (o.total_credits_burned ?? 0).toLocaleString() : '…'} accent />
          <StatCard label="Total loaded cost (USD)" value={o ? usdFromMicro(o.total_loaded_cost_usd_micro ?? 0) : '…'} />
          <StatCard label="Total raw cost (USD)" value={o ? usdFromMicro(o.total_raw_cost_usd_micro ?? 0) : '…'} />
        </div>
      </section>

      <section className="space-y-2">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">AI</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard label="Calls" value={o ? String(o.calls) : '…'} />
          <StatCard label="Credits" value={ai ? ai.credits_burned.toLocaleString() : (o ? String(o.credits_burned) : '…')} />
          <StatCard label="Raw cost (USD)" value={ai ? usdFromMicro(ai.raw_cost_usd_micro) : (o ? usdFromMicro(o.raw_cost_usd_micro) : '…')} />
          <StatCard label="Loaded cost (USD)" value={ai ? usdFromMicro(ai.loaded_cost_usd_micro) : (o ? usdFromMicro(o.loaded_cost_usd_micro) : '…')} />
          <StatCard label="Avg raw $/call" value={o ? o.avg_raw_cost_per_call_usd.toFixed(6) : '…'} />
          <StatCard label="Error rate" value={errRate != null ? pct(errRate) : o ? 'No calls' : '…'} />
        </div>
      </section>

      {res && (
        <section className="space-y-2">
          <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Resource usage</h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard label="Credits" value={res.credits_burned.toLocaleString()} />
            <StatCard label="Raw cost (USD)" value={usdFromMicro(res.raw_cost_usd_micro)} />
            <StatCard label="Loaded cost (USD)" value={usdFromMicro(res.loaded_cost_usd_micro)} />
            <StatCard label="Browser events" value={String(res.browser_events)} />
            <StatCard label="Browser active time" value={res.browser_duration_ms > 0 ? `${(res.browser_duration_ms / 60_000).toFixed(1)} min` : '0'} />
            <StatCard label="Render events" value={String(res.render_events)} />
          </div>
        </section>
      )}

      <EconomicsTable title="By task" loading={tasksQ.isLoading}>
        <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
          <tr>
            <th className="px-4 py-2 font-medium">Task</th>
            <th className="px-4 py-2 font-medium">Calls</th>
            <th className="px-4 py-2 font-medium">Raw USD</th>
            <th className="px-4 py-2 font-medium">Loaded USD</th>
            <th className="px-4 py-2 font-medium">Credits</th>
            <th className="px-4 py-2 font-medium">Avg latency</th>
            <th className="px-4 py-2 font-medium">Error rate</th>
          </tr>
        </thead>
        <tbody>
          {(tasksQ.data?.tasks ?? []).map(row => (
            <tr key={row.task} className="border-t border-[var(--color-border-subtle)]">
              <td className="px-4 py-2 font-mono text-xs">{row.task}</td>
              <td className="px-4 py-2 tabular-nums">{row.calls}</td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(row.raw_cost_usd_micro)}</td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(row.loaded_cost_usd_micro)}</td>
              <td className="px-4 py-2 tabular-nums">{row.credits_burned}</td>
              <td className="px-4 py-2 tabular-nums">{Math.round(row.avg_latency_ms)} ms</td>
              <td className="px-4 py-2 tabular-nums">
                {row.error_rate_available && row.error_rate != null ? pct(row.error_rate) : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </EconomicsTable>

      <EconomicsTable title="By model (requested × actual)" loading={modelsQ.isLoading}>
        <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
          <tr>
            <th className="px-4 py-2 font-medium">Requested</th>
            <th className="px-4 py-2 font-medium">Actual</th>
            <th className="px-4 py-2 font-medium">Calls</th>
            <th className="px-4 py-2 font-medium">Tokens in/out</th>
            <th className="px-4 py-2 font-medium">Raw USD</th>
            <th className="px-4 py-2 font-medium">Loaded USD</th>
            <th className="px-4 py-2 font-medium">Avg $/call</th>
            <th className="px-4 py-2 font-medium">Failures</th>
          </tr>
        </thead>
        <tbody>
          {(modelsQ.data?.models ?? []).map((row, i) => (
            <tr key={i} className="border-t border-[var(--color-border-subtle)]">
              <td className="px-4 py-2 text-xs">{String(row.requested_model)}</td>
              <td className="px-4 py-2 text-xs">{String(row.actual_model)}</td>
              <td className="px-4 py-2 tabular-nums">{String(row.calls)}</td>
              <td className="px-4 py-2 tabular-nums text-xs">
                {Number(row.input_tokens).toLocaleString()} / {Number(row.output_tokens).toLocaleString()}
              </td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(Number(row.raw_cost_usd_micro))}</td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(Number(row.loaded_cost_usd_micro))}</td>
              <td className="px-4 py-2 tabular-nums">{Number(row.avg_cost_per_call_usd).toFixed(6)}</td>
              <td className="px-4 py-2 tabular-nums">{String(row.failures)}</td>
            </tr>
          ))}
        </tbody>
      </EconomicsTable>

      <EconomicsTable title="By user (top 100)" loading={usersQ.isLoading}>
        <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
          <tr>
            <th className="px-4 py-2 font-medium">User ID</th>
            <th className="px-4 py-2 font-medium">Calls</th>
            <th className="px-4 py-2 font-medium">Raw USD</th>
            <th className="px-4 py-2 font-medium">Loaded USD</th>
            <th className="px-4 py-2 font-medium">Credits</th>
          </tr>
        </thead>
        <tbody>
          {(usersQ.data?.users ?? []).map((row, i) => (
            <tr key={i} className="border-t border-[var(--color-border-subtle)]">
              <td className="px-4 py-2 font-mono text-xs">{String(row.user_id)}</td>
              <td className="px-4 py-2 tabular-nums">{String(row.calls)}</td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(Number(row.raw_cost_usd_micro))}</td>
              <td className="px-4 py-2 tabular-nums">{usdFromMicro(Number(row.loaded_cost_usd_micro))}</td>
              <td className="px-4 py-2 tabular-nums">{String(row.credits_burned)}</td>
            </tr>
          ))}
        </tbody>
      </EconomicsTable>
    </div>
  )
}

function EconomicsTable({
  title,
  loading,
  children,
}: {
  title: string
  loading: boolean
  children: ReactNode
}) {
  return (
    <section className="space-y-2">
      <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">{title}</h2>
      <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
        <table className="w-full text-sm min-w-[640px]">{children}</table>
        {loading && <p className="p-3 text-sm text-[var(--color-text-dim)]">Loading…</p>}
      </div>
    </section>
  )
}
