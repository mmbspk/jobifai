import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { adminApi } from '../../api/admin'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { Button } from '../../components/Button'
import { ADMIN_PERIODS, type AdminPeriod, usdFromMicro } from '../../lib/adminFormat'
import { inputClassName } from '../../components/ui/input'
import type { ResourceUsageEvent } from '../../types'

const RESOURCE_TYPE_LABELS: Record<string, string> = {
  browser_automation: 'Browser automation',
  document_render: 'Document rendering',
}

function resourceLabel(rt: string): string {
  return RESOURCE_TYPE_LABELS[rt] ?? rt
}

const PAGE_SIZE = 50

export function AdminResourceUsagePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const period = (searchParams.get('period') as AdminPeriod) || '30d'
  const offset = Number(searchParams.get('offset') || '0')

  const [draft, setDraft] = useState({
    user_id: searchParams.get('user_id') ?? '',
    resource_type: searchParams.get('resource_type') ?? '',
  })

  const summaryQ = useQuery({
    queryKey: ['admin-resource-summary', period],
    queryFn: () => adminApi.resourceUsage.summary(period),
  })

  const eventsQ = useQuery({
    queryKey: ['admin-resource-events', period, offset, searchParams.get('user_id'), searchParams.get('resource_type')],
    queryFn: () => adminApi.resourceUsage.events({
      period,
      user_id: searchParams.get('user_id') ?? undefined,
      resource_type: searchParams.get('resource_type') ?? undefined,
      limit: PAGE_SIZE,
      offset,
    }),
  })

  const applyFilters = () => {
    const next = new URLSearchParams()
    next.set('period', period)
    next.set('offset', '0')
    if (draft.user_id) next.set('user_id', draft.user_id)
    if (draft.resource_type) next.set('resource_type', draft.resource_type)
    setSearchParams(next)
  }

  const s = summaryQ.data
  const total = eventsQ.data?.total ?? 0
  const ba = s?.browser_automation
  const dr = s?.document_render

  return (
    <div className="space-y-6">
      <PageHeader
        title="Resource usage"
        description="Browser automation and document rendering audit log."
      />

      <div className="flex flex-wrap gap-2">
        {ADMIN_PERIODS.map(p => (
          <button
            key={p.value}
            type="button"
            onClick={() => {
              const next = new URLSearchParams(searchParams)
              next.set('period', p.value)
              next.set('offset', '0')
              setSearchParams(next)
            }}
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

      {summaryQ.isError && (
        <p className="text-sm text-[var(--color-danger)]">Could not load resource usage summary.</p>
      )}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <StatCard label="Total credits burned" value={s ? s.total_credits_burned.toLocaleString() : '…'} accent />
        <StatCard label="Browser automation credits" value={ba ? ba.credits_burned.toLocaleString() : '0'} />
        <StatCard label="Document rendering credits" value={dr ? dr.credits_burned.toLocaleString() : '0'} />
        {ba && (
          <>
            <StatCard label="Browser events" value={String(ba.events)} />
            <StatCard label="Browser active time" value={ba.duration_ms ? `${((ba.duration_ms ?? 0) / 60_000).toFixed(1)} min` : '0'} />
          </>
        )}
        {dr && (
          <StatCard label="Document renders" value={String(dr.events)} />
        )}
      </div>

      <details className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-4">
        <summary className="cursor-pointer text-sm font-semibold">
          Filters{(draft.user_id || draft.resource_type) ? ' · active' : ''}
        </summary>
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          <label className="block text-xs text-[var(--color-text-muted)]">
            User ID
            <input
              className={inputClassName + ' mt-1 w-full'}
              value={draft.user_id}
              onChange={e => setDraft(d => ({ ...d, user_id: e.target.value }))}
              placeholder="Filter by user UUID"
            />
          </label>
          <label className="block text-xs text-[var(--color-text-muted)]">
            Resource type
            <select
              className={inputClassName + ' mt-1 w-full'}
              value={draft.resource_type}
              onChange={e => setDraft(d => ({ ...d, resource_type: e.target.value }))}
            >
              <option value="">All types</option>
              <option value="browser_automation">Browser automation</option>
              <option value="document_render">Document rendering</option>
            </select>
          </label>
        </div>
        <Button variant="secondary" className="mt-3" onClick={applyFilters}>
          Apply filters
        </Button>
      </details>

      <section className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
        <table className="w-full text-sm min-w-[1100px]">
          <thead className="sticky top-0 bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
            <tr>
              <th className="px-2 py-2">Time</th>
              <th className="px-2 py-2">User</th>
              <th className="px-2 py-2">Resource</th>
              <th className="px-2 py-2">Qty</th>
              <th className="px-2 py-2">Duration</th>
              <th className="px-2 py-2">Credits</th>
              <th className="px-2 py-2">Raw USD</th>
              <th className="px-2 py-2">Loaded USD</th>
              <th className="px-2 py-2">OK</th>
              <th className="px-2 py-2">Error</th>
              <th className="px-2 py-2">Automation</th>
              <th className="px-2 py-2">App</th>
              <th className="px-2 py-2">Job</th>
            </tr>
          </thead>
          <tbody>
            {(eventsQ.data?.events ?? []).map((ev: ResourceUsageEvent) => (
              <tr key={ev.id} className="border-t border-[var(--color-border-subtle)]">
                <td className="px-2 py-1.5 whitespace-nowrap text-xs">{ev.created_at.slice(0, 19)}</td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[80px] truncate" title={ev.user_id}>
                  {ev.user_id.slice(0, 8)}
                </td>
                <td className="px-2 py-1.5 text-xs">{resourceLabel(ev.resource_type)}</td>
                <td className="px-2 py-1.5 tabular-nums text-xs">{ev.quantity} {ev.unit}</td>
                <td className="px-2 py-1.5 tabular-nums text-xs">
                  {ev.duration_ms > 0 ? `${ev.duration_ms} ms` : '—'}
                </td>
                <td className="px-2 py-1.5 tabular-nums">{ev.credits_burned}</td>
                <td className="px-2 py-1.5 tabular-nums text-xs">{usdFromMicro(ev.raw_cost_usd_micro)}</td>
                <td className="px-2 py-1.5 tabular-nums text-xs">{usdFromMicro(ev.loaded_cost_usd_micro)}</td>
                <td className="px-2 py-1.5">{ev.success ? '✓' : '✗'}</td>
                <td className="px-2 py-1.5 text-xs">{ev.error_code || '—'}</td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[72px] truncate" title={ev.automation_run_id}>
                  {ev.automation_run_id?.slice(0, 8) || '—'}
                </td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[72px] truncate" title={ev.application_id}>
                  {ev.application_id?.slice(0, 8) || '—'}
                </td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[72px] truncate" title={ev.job_id}>
                  {ev.job_id?.slice(0, 8) || '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {eventsQ.isLoading && <p className="p-4 text-sm text-[var(--color-text-dim)]">Loading…</p>}
        {eventsQ.isError && <p className="p-4 text-sm text-[var(--color-danger)]">Could not load resource usage events.</p>}
        {!eventsQ.isLoading && !eventsQ.isError && (eventsQ.data?.events?.length ?? 0) === 0 && (
          <p className="p-4 text-sm text-[var(--color-text-dim)]">No resource usage events found for this period.</p>
        )}
      </section>

      <div className="flex items-center gap-3">
        <Button
          variant="secondary"
          disabled={offset <= 0}
          onClick={() => {
            const next = new URLSearchParams(searchParams)
            next.set('offset', String(Math.max(0, offset - PAGE_SIZE)))
            setSearchParams(next)
          }}
        >
          Previous
        </Button>
        <span className="text-sm text-[var(--color-text-muted)]">
          {total === 0 ? '0 events' : `${offset + 1}–${Math.min(offset + PAGE_SIZE, total)} of ${total}`}
        </span>
        <Button
          variant="secondary"
          disabled={offset + PAGE_SIZE >= total}
          onClick={() => {
            const next = new URLSearchParams(searchParams)
            next.set('offset', String(offset + PAGE_SIZE))
            setSearchParams(next)
          }}
        >
          Next
        </Button>
      </div>
    </div>
  )
}
