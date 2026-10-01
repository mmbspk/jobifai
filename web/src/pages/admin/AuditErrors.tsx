import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { StatCard } from '../../components/StatCard'
import { ADMIN_PERIODS, type AdminPeriod } from '../../lib/adminFormat'
import { DialogRoot, DialogContent } from '../../components/ui/dialog'
import { Badge } from '../../components/ui/badge'

function localTime(value: unknown): string {
  const date = new Date(String(value ?? ''))
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString()
}

function scoreLabel(bucket: string): string {
  return `Score ${bucket}`
}

export function AdminAuditErrorsPage() {
  const [period, setPeriod] = useState<AdminPeriod>('7d')
  const [auditTask, setAuditTask] = useState('')
  const [selectedScore, setSelectedScore] = useState<Record<string, unknown> | null>(null)

  const errorsQ = useQuery({
    queryKey: ['admin-operational-errors', period],
    queryFn: () => apiGet<{ errors: Record<string, string>[] }>(`/admin/operational-errors?period=${period}`),
  })

  const auditQ = useQuery({
    queryKey: ['admin-policy-audit', auditTask],
    queryFn: () =>
      apiGet<{ audit: Record<string, string>[] }>(
        `/admin/models/policy-audit${auditTask ? `?task=${encodeURIComponent(auditTask)}` : ''}`,
      ),
  })

  const jobsSummaryQ = useQuery({
    queryKey: ['admin-jobs-summary'],
    queryFn: () => apiGet<Record<string, number>>('/admin/operations/jobs/summary'),
  })

  const jobsRecentQ = useQuery({
    queryKey: ['admin-jobs-recent'],
    queryFn: () => apiGet<{ jobs: Record<string, unknown>[] }>('/admin/operations/jobs/recent?limit=40'),
  })

  const scoringQ = useQuery({
    queryKey: ['admin-scoring-recent'],
    queryFn: () =>
      apiGet<{ items: Record<string, unknown>[]; score_distribution: Record<string, number> }>(
        '/admin/operations/scoring/recent?limit=40',
      ),
  })

  const js = jobsSummaryQ.data
  const scoreBuckets = Object.entries(scoringQ.data?.score_distribution ?? {}).sort(([a], [b]) => Number.parseInt(a) - Number.parseInt(b))
  const scoreMax = Math.max(1, ...scoreBuckets.map(([, count]) => count))

  return (
    <div className="space-y-8">
      <PageHeader
        title="Audit & operations"
        description="Trace application decisions, review errors, and inspect policy changes."
      />

      <section className="space-y-3">
        <div><h2 className="text-base font-semibold">Job pipeline</h2><p className="text-sm text-[var(--color-text-muted)]">Application outcomes across all users.</p></div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard label="Applied" value={js ? String(js.applied) : '…'} />
          <StatCard label="Skipped" value={js ? String(js.skipped) : '…'} />
          <StatCard label="Cannot apply" value={js ? String(js.cannot_apply) : '…'} />
          <StatCard label="Pending review" value={js ? String(js.pending_review) : '…'} />
        </div>
        {js && <p className="text-xs text-[var(--color-text-muted)]">{js.top_matches ?? 0} top matches · {js.approved_queue ?? 0} approved and queued</p>}
      </section>

      <section className="space-y-4">
        <div><h2 className="text-base font-semibold">Recent scoring</h2><p className="text-sm text-[var(--color-text-muted)]">Scores from the most recent sample, followed by individual decisions.</p></div>
        <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-5">
          <h3 className="mb-4 text-sm font-semibold">Score distribution</h3>
          {scoreBuckets.length === 0 && <p className="text-sm text-[var(--color-text-dim)]">No recent scores available.</p>}
          <div className="space-y-3">
            {scoreBuckets.map(([bucket, count]) => <div key={bucket} className="grid grid-cols-[72px_minmax(0,1fr)_36px] items-center gap-3 text-xs sm:grid-cols-[90px_minmax(0,1fr)_36px]">
              <span className="text-[var(--color-text-muted)]">{scoreLabel(bucket)}</span>
              <div className="h-2.5 overflow-hidden rounded-full bg-[var(--color-surface-2)]" role="meter" aria-label={`${scoreLabel(bucket)} count`} aria-valuemin={0} aria-valuemax={scoreMax} aria-valuenow={count}>
                <div className="h-full rounded-full bg-[var(--color-admin)]" style={{ width: `${count / scoreMax * 100}%` }} />
              </div>
              <span className="text-right font-semibold tabular-nums">{count}</span>
            </div>)}
          </div>
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[900px]">
            <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-3 py-2">Time</th>
                <th className="px-3 py-2">Company / role</th>
                <th className="px-3 py-2">Score</th>
                <th className="px-3 py-2">Bucket</th>
                <th className="px-3 py-2">Model</th>
                <th className="px-3 py-2">Reasoning</th>
              </tr>
            </thead>
            <tbody>
              {(scoringQ.data?.items ?? []).map((row, i) => (
                <tr key={i} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-3 py-2 text-xs whitespace-nowrap">{localTime(row.created_at)}</td>
                  <td className="px-3 py-2">
                    {String(row.company)} — {String(row.role)}
                  </td>
                  <td className="px-3 py-2 tabular-nums font-medium">{String(row.score)}</td>
                  <td className="px-3 py-2 text-xs"><Badge variant="muted">{String(row.decision_bucket)}</Badge></td>
                  <td className="px-3 py-2 text-xs">{String(row.scoring_model || '—')}</td>
                  <td className="px-3 py-2 text-xs max-w-md">
                    {row.reasoning ? <button type="button" className="block max-w-[240px] truncate text-left text-[var(--color-admin)] hover:underline" onClick={() => setSelectedScore(row)} aria-label={`View reasoning for ${String(row.company)} ${String(row.role)}`}>
                      {String(row.reasoning)}
                    </button> : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <DialogRoot open={!!selectedScore} onOpenChange={open => !open && setSelectedScore(null)}>
          <DialogContent title="Scoring decision" className="max-w-xl">
            {selectedScore && <div className="space-y-4 text-sm">
              <p className="font-semibold">{String(selectedScore.company)} — {String(selectedScore.role)}</p>
              <div className="flex flex-wrap gap-2"><Badge variant="admin">Score {String(selectedScore.score)}</Badge><Badge variant="muted">{String(selectedScore.decision_bucket)}</Badge></div>
              <div><h3 className="mb-1 font-semibold">Reasoning</h3><p className="max-h-[50vh] overflow-y-auto whitespace-pre-wrap break-words text-[var(--color-text-muted)]">{String(selectedScore.reasoning)}</p></div>
            </div>}
          </DialogContent>
        </DialogRoot>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Recent jobs</h2>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[800px]">
            <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-3 py-2">Time</th>
                <th className="px-3 py-2">User</th>
                <th className="px-3 py-2">Platform</th>
                <th className="px-3 py-2">Company</th>
                <th className="px-3 py-2">Score</th>
                <th className="px-3 py-2">State</th>
              </tr>
            </thead>
            <tbody>
              {(jobsRecentQ.data?.jobs ?? []).map((row, i) => (
                <tr key={i} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-3 py-2 text-xs">{String(row.created_at).slice(0, 19)}</td>
                  <td className="px-3 py-2 font-mono text-xs">{String(row.user_id).slice(0, 8)}</td>
                  <td className="px-3 py-2">{String(row.platform)}</td>
                  <td className="px-3 py-2">{String(row.company)}</td>
                  <td className="px-3 py-2 tabular-nums">{String(row.score)}</td>
                  <td className="px-3 py-2">{String(row.state)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Operational errors</h2>
          {ADMIN_PERIODS.map(p => (
            <button
              key={p.value}
              type="button"
              onClick={() => setPeriod(p.value)}
              className={
                period === p.value
                  ? 'text-xs rounded border border-[var(--color-admin)]/40 px-2 py-1 bg-[var(--color-admin-soft)]'
                  : 'text-xs rounded border border-[var(--color-border)] px-2 py-1'
              }
            >
              {p.label}
            </button>
          ))}
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[700px]">
            <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-3 py-2">Time</th>
                <th className="px-3 py-2">Subsystem</th>
                <th className="px-3 py-2">Task</th>
                <th className="px-3 py-2">Code</th>
                <th className="px-3 py-2">Message</th>
                <th className="px-3 py-2">User</th>
              </tr>
            </thead>
            <tbody>
              {(errorsQ.data?.errors ?? []).map((row, i) => (
                <tr key={i} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-3 py-2 text-xs">{row.timestamp?.slice(0, 19)}</td>
                  <td className="px-3 py-2">{row.subsystem}</td>
                  <td className="px-3 py-2 font-mono text-xs">{row.task || '—'}</td>
                  <td className="px-3 py-2 text-red-600 dark:text-red-400">{row.error_code || '—'}</td>
                  <td className="px-3 py-2 max-w-xs truncate">{row.message}</td>
                  <td className="px-3 py-2 font-mono text-xs">{row.user_id?.slice(0, 8) ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Model policy audit</h2>
          <input
            className="text-sm rounded border border-[var(--color-border)] px-2 py-1"
            placeholder="Filter task…"
            value={auditTask}
            onChange={e => setAuditTask(e.target.value)}
          />
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-sm min-w-[800px]">
            <thead className="bg-[var(--color-surface-2)] text-left text-[var(--color-text-muted)]">
              <tr>
                <th className="px-3 py-2">Time</th>
                <th className="px-3 py-2">Task</th>
                <th className="px-3 py-2">By</th>
                <th className="px-3 py-2">Action</th>
                <th className="px-3 py-2">Previous</th>
                <th className="px-3 py-2">New</th>
                <th className="px-3 py-2">Eval run</th>
              </tr>
            </thead>
            <tbody>
              {(auditQ.data?.audit ?? []).map((row, i) => (
                <tr key={i} className="border-t border-[var(--color-border-subtle)]">
                  <td className="px-3 py-2 text-xs">{row.timestamp?.slice(0, 19)}</td>
                  <td className="px-3 py-2 font-mono text-xs">{row.task}</td>
                  <td className="px-3 py-2 text-xs">{row.changed_by}</td>
                  <td className="px-3 py-2 text-xs">{row.action}</td>
                  <td className="px-3 py-2 text-xs">{row.previous}</td>
                  <td className="px-3 py-2 text-xs">{row.new}</td>
                  <td className="px-3 py-2 font-mono text-xs max-w-[100px] truncate" title={row.eval_run_id}>
                    {row.eval_run_id?.slice(0, 8) ?? '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
