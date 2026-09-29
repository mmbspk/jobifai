import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'

type Overview = {
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  credits_burned: number
  calls: number
  avg_raw_cost_per_call_usd: number
}

type TaskRow = {
  task: string
  calls: number
  raw_cost_usd_micro: number
  credits_burned: number
  error_rate?: number
  error_rate_available: boolean
}

export function AdminEconomicsPage() {
  const overviewQ = useQuery({
    queryKey: ['admin-economics-overview'],
    queryFn: () => apiGet<Overview>('/admin/economics/overview?days=30'),
  })
  const tasksQ = useQuery({
    queryKey: ['admin-economics-tasks'],
    queryFn: () => apiGet<{ tasks: TaskRow[]; failure_tracking_enabled: boolean }>('/admin/economics/tasks?days=30'),
  })

  const o = overviewQ.data
  const usd = (micro: number) => (micro / 1_000_000).toFixed(4)

  return (
    <div className="space-y-6">
      <PageHeader
        title="AI economics"
        description="Billing-grade LLM usage from the event ledger (no prompt or resume content)."
      />
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Calls (30d)" value={o ? String(o.calls) : '…'} />
        <StatCard label="Raw AI cost (USD)" value={o ? usd(o.raw_cost_usd_micro) : '…'} />
        <StatCard label="Loaded cost (USD)" value={o ? usd(o.loaded_cost_usd_micro) : '…'} />
        <StatCard label="Credits burned" value={o ? o.credits_burned.toLocaleString() : '…'} />
      </div>
      <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
            <tr>
              <th className="px-4 py-2 font-medium">Task</th>
              <th className="px-4 py-2 font-medium">Calls</th>
              <th className="px-4 py-2 font-medium">Raw USD</th>
              <th className="px-4 py-2 font-medium">Credits</th>
              <th className="px-4 py-2 font-medium">Error rate (tracked failures)</th>
            </tr>
          </thead>
          <tbody>
            {(tasksQ.data?.tasks ?? []).map(row => (
              <tr key={row.task} className="border-t border-[var(--color-border-subtle)]">
                <td className="px-4 py-2 font-mono text-xs">{row.task}</td>
                <td className="px-4 py-2 tabular-nums">{row.calls}</td>
                <td className="px-4 py-2 tabular-nums">{usd(row.raw_cost_usd_micro)}</td>
                <td className="px-4 py-2 tabular-nums">{row.credits_burned}</td>
                <td className="px-4 py-2 tabular-nums">
                  {row.error_rate_available && row.error_rate != null
                    ? `${(row.error_rate * 100).toFixed(1)}%`
                    : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  )
}
