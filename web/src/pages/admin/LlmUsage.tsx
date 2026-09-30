import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { Button } from '../../components/Button'
import { ADMIN_PERIODS, type AdminPeriod, pct, usdFromMicro } from '../../lib/adminFormat'
import { inputClassName } from '../../components/ui/input'

type Summary = {
  calls: number
  success_calls: number
  error_rate: number
  input_tokens: number
  output_tokens: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  credits_burned: number
  avg_latency_ms: number
  p90_latency_ms: number
  breakdown: Record<string, unknown>[]
}

type EventRow = {
  id: string
  created_at: string
  user_id: string
  task: string
  requested_model: string
  actual_model: string
  actual_model_verified: boolean
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  credits_burned: number
  latency_ms: number
  success: boolean
  error_code?: string
  job_id?: string
  automation_run_id?: string
}

function buildQuery(base: Record<string, string>, period: AdminPeriod, offset: number, groupBy: string) {
  const p = new URLSearchParams(base)
  p.set('period', period)
  p.set('offset', String(offset))
  p.set('limit', '50')
  if (groupBy) p.set('group_by', groupBy)
  return p.toString()
}

export function AdminLlmUsagePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const period = (searchParams.get('period') as AdminPeriod) || '30d'
  const offset = Number(searchParams.get('offset') || '0')
  const groupBy = searchParams.get('group_by') || 'task_model'

  const [draft, setDraft] = useState({
    user_id: searchParams.get('user_id') ?? '',
    task: searchParams.get('task') ?? '',
    provider: searchParams.get('provider') ?? '',
    requested_model: searchParams.get('requested_model') ?? '',
    actual_model: searchParams.get('actual_model') ?? '',
    success: searchParams.get('success') ?? '',
    error_code: searchParams.get('error_code') ?? '',
    job_id: searchParams.get('job_id') ?? '',
    automation_run_id: searchParams.get('automation_run_id') ?? '',
  })

  const filterParams = useMemo(() => {
    const keys = [
      'user_id',
      'task',
      'provider',
      'requested_model',
      'actual_model',
      'success',
      'error_code',
      'job_id',
      'automation_run_id',
    ] as const
    const f: Record<string, string> = {}
    for (const k of keys) {
      const v = searchParams.get(k)
      if (v) f[k] = v
    }
    return f
  }, [searchParams])

  const eventsQ = useQuery({
    queryKey: ['admin-llm-events', period, offset, filterParams],
    queryFn: () =>
      apiGet<{ events: EventRow[]; total: number }>(
        `/admin/llm-usage/events?${buildQuery(filterParams, period, offset, '')}`,
      ),
  })

  const summaryQ = useQuery({
    queryKey: ['admin-llm-summary', period, filterParams, groupBy],
    queryFn: () =>
      apiGet<Summary>(`/admin/llm-usage/summary?${buildQuery(filterParams, period, 0, groupBy)}`),
  })

  const applyFilters = () => {
    const next = new URLSearchParams(searchParams)
    next.set('period', period)
    next.set('offset', '0')
    for (const [k, v] of Object.entries(draft)) {
      if (v) next.set(k, v)
      else next.delete(k)
    }
    setSearchParams(next)
  }

  const s = summaryQ.data
  const total = eventsQ.data?.total ?? 0

  return (
    <div className="space-y-6">
      <PageHeader
        title="AI usage"
        description="LLM call ledger — filters and aggregates only; no prompts or resume content."
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

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <StatCard label="Calls" value={s ? String(s.calls) : '…'} />
        <StatCard label="Success" value={s ? String(s.success_calls) : '…'} />
        <StatCard label="Error rate" value={s ? pct(s.error_rate) : '…'} />
        <StatCard label="Raw USD" value={s ? usdFromMicro(s.raw_cost_usd_micro) : '…'} />
        <StatCard label="Loaded USD" value={s ? usdFromMicro(s.loaded_cost_usd_micro) : '…'} />
        <StatCard label="Credits" value={s ? s.credits_burned.toLocaleString() : '…'} />
        <StatCard label="Input tokens" value={s ? s.input_tokens.toLocaleString() : '…'} />
        <StatCard label="Output tokens" value={s ? s.output_tokens.toLocaleString() : '…'} />
        <StatCard label="Avg latency" value={s ? `${Math.round(s.avg_latency_ms)} ms` : '…'} />
        <StatCard label="P90 latency" value={s ? `${Math.round(s.p90_latency_ms)} ms` : '…'} />
      </div>

      <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] p-4 space-y-3">
        <h2 className="text-sm font-semibold">Filters</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {(
            [
              ['user_id', 'User ID'],
              ['task', 'Task'],
              ['provider', 'Provider'],
              ['requested_model', 'Requested model'],
              ['actual_model', 'Actual model'],
              ['success', 'Success (true/false)'],
              ['error_code', 'Error code'],
              ['job_id', 'Job ID'],
              ['automation_run_id', 'Automation run ID'],
            ] as const
          ).map(([key, label]) => (
            <label key={key} className="block text-xs text-[var(--color-text-muted)]">
              {label}
              <input
                className={inputClassName + ' mt-1 w-full'}
                value={draft[key]}
                onChange={e => setDraft(d => ({ ...d, [key]: e.target.value }))}
              />
            </label>
          ))}
        </div>
        <Button variant="secondary" onClick={applyFilters}>
          Apply filters
        </Button>
      </section>

      <section className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-[var(--color-text-muted)]">Breakdown by</span>
          {(
            [
              ['task', 'Task'],
              ['model', 'Model'],
              ['user', 'User'],
              ['task_model', 'Task + models'],
            ] as const
          ).map(([v, label]) => (
            <button
              key={v}
              type="button"
              onClick={() => {
                const next = new URLSearchParams(searchParams)
                next.set('group_by', v)
                setSearchParams(next)
              }}
              className={
                groupBy === v
                  ? 'text-xs rounded border border-[var(--color-admin)]/40 px-2 py-1 bg-[var(--color-admin-soft)]'
                  : 'text-xs rounded border border-[var(--color-border)] px-2 py-1'
              }
            >
              {label}
            </button>
          ))}
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[480px]">
            <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-3 py-2">Group</th>
                <th className="px-3 py-2">Calls</th>
                <th className="px-3 py-2">Raw USD</th>
                <th className="px-3 py-2">Loaded USD</th>
                <th className="px-3 py-2">Credits</th>
              </tr>
            </thead>
            <tbody>
              {(s?.breakdown ?? []).map((row, i) => (
                <tr key={i} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-3 py-2 font-mono text-xs max-w-xs truncate">
                    {Object.entries(row)
                      .filter(([k]) => !['calls', 'raw_cost_usd_micro', 'loaded_cost_usd_micro', 'credits_burned', 'avg_latency_ms', 'failures'].includes(k))
                      .map(([, v]) => String(v))
                      .join(' · ')}
                  </td>
                  <td className="px-3 py-2 tabular-nums">{String(row.calls ?? '')}</td>
                  <td className="px-3 py-2 tabular-nums">{usdFromMicro(Number(row.raw_cost_usd_micro))}</td>
                  <td className="px-3 py-2 tabular-nums">{usdFromMicro(Number(row.loaded_cost_usd_micro))}</td>
                  <td className="px-3 py-2 tabular-nums">{String(row.credits_burned ?? '')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
        <table className="w-full text-sm min-w-[1200px]">
          <thead className="sticky top-0 bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
            <tr>
              <th className="px-2 py-2">Time</th>
              <th className="px-2 py-2">User</th>
              <th className="px-2 py-2">Task</th>
              <th className="px-2 py-2">Requested</th>
              <th className="px-2 py-2">Actual</th>
              <th className="px-2 py-2">✓</th>
              <th className="px-2 py-2">In/out</th>
              <th className="px-2 py-2">Cost</th>
              <th className="px-2 py-2">Cr</th>
              <th className="px-2 py-2">ms</th>
              <th className="px-2 py-2">OK</th>
              <th className="px-2 py-2">Err</th>
              <th className="px-2 py-2">Job</th>
            </tr>
          </thead>
          <tbody>
            {(eventsQ.data?.events ?? []).map(ev => (
              <tr key={ev.id} className="border-t border-[var(--color-border-subtle)]">
                <td className="px-2 py-1.5 whitespace-nowrap text-xs">{ev.created_at.slice(0, 19)}</td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[80px] truncate" title={ev.user_id}>
                  {ev.user_id.slice(0, 8)}
                </td>
                <td className="px-2 py-1.5 font-mono text-xs">{ev.task}</td>
                <td className="px-2 py-1.5 text-xs">{ev.requested_model}</td>
                <td className="px-2 py-1.5 text-xs">{ev.actual_model}</td>
                <td className="px-2 py-1.5">{ev.actual_model_verified ? '✓' : '—'}</td>
                <td className="px-2 py-1.5 tabular-nums text-xs">
                  {ev.input_tokens}/{ev.output_tokens}
                  {ev.cache_read_tokens > 0 ? ` (+${ev.cache_read_tokens}c)` : ''}
                </td>
                <td className="px-2 py-1.5 tabular-nums text-xs">{usdFromMicro(ev.raw_cost_usd_micro)}</td>
                <td className="px-2 py-1.5 tabular-nums">{ev.credits_burned}</td>
                <td className="px-2 py-1.5 tabular-nums">{ev.latency_ms}</td>
                <td className="px-2 py-1.5">{ev.success ? '✓' : '✗'}</td>
                <td className="px-2 py-1.5 text-xs">{ev.error_code || '—'}</td>
                <td className="px-2 py-1.5 font-mono text-xs max-w-[72px] truncate" title={ev.job_id}>
                  {ev.job_id?.slice(0, 8) ?? '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {eventsQ.isLoading && <p className="p-4 text-sm text-[var(--color-text-dim)]">Loading…</p>}
        {!eventsQ.isLoading && (eventsQ.data?.events?.length ?? 0) === 0 && (
          <p className="p-4 text-sm text-[var(--color-text-dim)]">No events for this filter.</p>
        )}
      </section>

      <div className="flex items-center gap-3">
        <Button
          variant="secondary"
          disabled={offset <= 0}
          onClick={() => {
            const next = new URLSearchParams(searchParams)
            next.set('offset', String(Math.max(0, offset - 50)))
            setSearchParams(next)
          }}
        >
          Previous
        </Button>
        <span className="text-sm text-[var(--color-text-muted)]">
          {offset + 1}–{Math.min(offset + 50, total)} of {total}
        </span>
        <Button
          variant="secondary"
          disabled={offset + 50 >= total}
          onClick={() => {
            const next = new URLSearchParams(searchParams)
            next.set('offset', String(offset + 50))
            setSearchParams(next)
          }}
        >
          Next
        </Button>
      </div>
    </div>
  )
}
