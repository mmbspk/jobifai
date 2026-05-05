import { useQuery } from '@tanstack/react-query'
import { usageApi } from '../../api/usage'

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-5 flex flex-col gap-1">
      <div className="text-xs font-semibold text-[var(--color-text-muted)] uppercase tracking-wider">
        {label}
      </div>
      <div className="text-2xl font-bold text-[var(--color-text)]">{value}</div>
    </div>
  )
}

function fmt(n: number): string {
  return n.toLocaleString()
}

export function UsagePage() {
  const { data, isLoading } = useQuery({
    queryKey: ['usage', 'totals'],
    queryFn: usageApi.totals,
    refetchInterval: 30_000,
  })

  if (isLoading || !data) {
    return (
      <div className="text-sm text-[var(--color-text-dim)]">Loading usage statistics…</div>
    )
  }

  const cost =
    data.estimated_cost_usd != null
      ? `$${data.estimated_cost_usd.toFixed(2)}`
      : '—'

  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--color-border)] text-xs font-medium text-[var(--color-text-muted)] uppercase tracking-wider">
          Usage Statistics
        </div>
        <div className="p-4 grid grid-cols-2 gap-3">
          <StatCard label="Cost" value={cost} />
          <StatCard label="Total Requests" value={fmt(data.calls)} />
          <StatCard label="Input Tokens" value={fmt(data.input_tokens)} />
          <StatCard label="Output Tokens" value={fmt(data.output_tokens)} />
        </div>
      </div>
    </div>
  )
}
