import { useQuery } from '@tanstack/react-query'
import { usageApi } from '../../api/usage'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { SettingsSection } from '../../components/settings/settings-ui'

function fmt(n: number): string {
  return n.toLocaleString()
}

export function UsagePage() {
  const { data, isLoading } = useQuery({
    queryKey: ['usage', 'totals'],
    queryFn: usageApi.totals,
    refetchInterval: 30_000,
  })

  const cost =
    data?.estimated_cost_usd != null
      ? `$${data.estimated_cost_usd.toFixed(2)}`
      : '—'

  return (
    <div className="space-y-6">
      <PageHeader
        title="Usage"
        description="LLM request volume and estimated cost for this deployment (refreshes every 30 seconds)."
      />

      {isLoading || !data ? (
        <p className="text-sm text-[var(--color-text-dim)]">Loading usage statistics…</p>
      ) : (
        <SettingsSection title="Totals">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <StatCard label="Estimated cost" value={cost} accent />
            <StatCard label="Total requests" value={fmt(data.calls)} />
            <StatCard label="Input tokens" value={fmt(data.input_tokens)} />
            <StatCard label="Output tokens" value={fmt(data.output_tokens)} />
          </div>
        </SettingsSection>
      )}
    </div>
  )
}
