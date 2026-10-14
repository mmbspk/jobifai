import { useQuery } from '@tanstack/react-query'
import { Bot, FileText, Zap } from 'lucide-react'
import { usageApi } from '../../api/usage'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { SettingsSection } from '../../components/settings/settings-ui'

function fmt(n: number): string {
  return n.toLocaleString()
}

function fmtMinutes(minutes: number): string {
  if (minutes < 1) return `${Math.round(minutes * 60)} sec`
  return `${minutes.toFixed(1)} min`
}

function fmtPeriod(start: string, end: string): string {
  try {
    const s = new Date(start).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    const e = new Date(end).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
    return `${s} – ${e}`
  } catch {
    return `${start} – ${end}`
  }
}

export function UsagePage() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['usage', 'summary'],
    queryFn: usageApi.summary,
    refetchInterval: 60_000,
  })

  if (isLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Usage" description="Credits used this billing period." />
        <p className="text-sm text-[var(--color-text-dim)]">Loading usage…</p>
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <PageHeader title="Usage" description="Credits used this billing period." />
        <p className="text-sm text-[var(--color-danger)]">Could not load usage data. Please try again.</p>
      </div>
    )
  }

  const periodLabel = data.period_start && data.period_end
    ? fmtPeriod(data.period_start, data.period_end)
    : 'Current period'

  return (
    <div className="space-y-6">
      <PageHeader
        title="Usage"
        description={`Credits used this billing period · ${periodLabel}`}
      />

      <SettingsSection
        title="This billing period"
        description="Credits cover AI processing and resource-intensive automation performed by Jobifai."
      >
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <StatCard
            label="Total credits used"
            value={fmt(data.total_credits_used)}
            accent
          />
        </div>

        {data.total_credits_used === 0 && (
          <p className="text-sm text-[var(--color-text-dim)] mt-2">
            No usage recorded this billing period yet.
          </p>
        )}
      </SettingsSection>

      <SettingsSection title="Breakdown">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <StatCard
            label="AI"
            value={`${fmt(data.ai_credits)} credits`}
            sub={`${fmt(data.ai_calls)} operations`}
            icon={<Zap size={18} />}
          />
          <StatCard
            label="Browser automation"
            value={`${fmt(data.browser_automation_credits)} credits`}
            sub={data.browser_active_minutes > 0
              ? `${fmtMinutes(data.browser_active_minutes)} active`
              : 'No automation this period'}
            icon={<Bot size={18} />}
          />
          <StatCard
            label="Document rendering"
            value={`${fmt(data.document_render_credits)} credits`}
            sub={data.document_renders > 0
              ? `${fmt(data.document_renders)} PDF${data.document_renders === 1 ? '' : 's'}`
              : 'No documents this period'}
            icon={<FileText size={18} />}
          />
        </div>
      </SettingsSection>
    </div>
  )
}
