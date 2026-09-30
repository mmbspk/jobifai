import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet, apiPost } from '../../api/client'
import { PageHeader } from '../../components/shell/PageHeader'
import { Button } from '../../components/Button'
import { Badge } from '../../components/ui/badge'
import { DialogRoot, DialogContent } from '../../components/ui/dialog'

type PolicyRow = { task: string; provider: string; model: string; mode: string; effort: string; state: string }
type PolicyDetail = {
  task: string
  model: string
  effort: string
  previous_state: string
  eval_run_id: string
  approved_by: string
  updated_at: string
  rollback_available: boolean
}
type EvalRow = {
  id: string
  task: string
  dataset: string
  status: string
  cost_micro: number
  created: string
  runner_type: string
  run_purpose: string
}
type EffectiveRow = {
  task: string
  provider: string
  model: string
  effort: string
  source: string
  max_tokens: number
  timeout_sec: number
  max_cost_usd: number
}
type RecRow = {
  id: string
  task: string
  outcome: string
  deployable: boolean
  approvable: boolean
  runner_type: string
  run_purpose: string
  reason: string
}

export function AdminModelsPage() {
  const qc = useQueryClient()
  const [evalDetailId, setEvalDetailId] = useState<string | null>(null)
  const [recReviewId, setRecReviewId] = useState<string | null>(null)
  const [rollbackTask, setRollbackTask] = useState<string | null>(null)
  const [rollbackMsg, setRollbackMsg] = useState('')

  const policies = useQuery({
    queryKey: ['admin', 'models', 'policies'],
    queryFn: () => apiGet<PolicyRow[]>('/admin/models/policies'),
  })
  const policiesDetail = useQuery({
    queryKey: ['admin', 'models', 'policies-detail'],
    queryFn: () => apiGet<PolicyDetail[]>('/admin/models/policies/detail'),
  })
  const effective = useQuery({
    queryKey: ['admin', 'models', 'effective'],
    queryFn: () => apiGet<EffectiveRow[]>('/admin/models/effective'),
  })
  const evals = useQuery({
    queryKey: ['admin', 'models', 'evals'],
    queryFn: () => apiGet<EvalRow[]>('/admin/models/evals'),
  })
  const recs = useQuery({
    queryKey: ['admin', 'models', 'recommendations'],
    queryFn: () => apiGet<RecRow[]>('/admin/models/recommendations'),
  })
  const catalog = useQuery({
    queryKey: ['admin', 'models', 'catalog'],
    queryFn: () => apiGet<{ discovery_source: string; last_refresh_at: string; candidates: unknown[] }>('/admin/models/catalog'),
  })

  const evalDetail = useQuery({
    queryKey: ['admin', 'eval-detail', evalDetailId],
    queryFn: () => apiGet<Record<string, unknown>>(`/admin/models/evals/${evalDetailId}/detail`),
    enabled: !!evalDetailId,
  })

  const recDetail = useQuery({
    queryKey: ['admin', 'rec-detail', recReviewId],
    queryFn: () => apiGet<Record<string, unknown>>(`/admin/models/recommendations/${recReviewId}`),
    enabled: !!recReviewId,
  })

  const refreshCatalog = useMutation({
    mutationFn: () => apiPost('/admin/models/catalog/refresh', {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'models', 'catalog'] }),
  })
  const cancelEval = useMutation({
    mutationFn: (id: string) => apiPost(`/admin/models/evals/${id}/cancel`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'models', 'evals'] }),
  })

  const openRollback = async (task: string) => {
    const preview = await apiGet<{ message: string }>(`/admin/models/policies/${task}/rollback-preview`)
    setRollbackMsg(preview.message)
    setRollbackTask(task)
  }

  const rollback = useMutation({
    mutationFn: (task: string) => apiPost(`/admin/models/policies/${task}/rollback`, {}),
    onSuccess: () => {
      setRollbackTask(null)
      qc.invalidateQueries({ queryKey: ['admin', 'models'] })
    },
  })

  const approve = useMutation({
    mutationFn: ({ task, recommendationId }: { task: string; recommendationId: string }) =>
      apiPost(`/admin/models/policies/${task}/approve`, { recommendation_id: recommendationId }),
    onSuccess: () => {
      setRecReviewId(null)
      qc.invalidateQueries({ queryKey: ['admin', 'models'] })
    },
  })

  const detailByTask = new Map((policiesDetail.data ?? []).map(d => [d.task, d]))

  return (
    <div className="space-y-8 text-sm">
      <PageHeader
        title="Models"
        description="Effective routing, task policies, evaluations, and approval controls."
      />

      <section>
        <h2 className="mb-2 font-medium text-[var(--color-text)]">Effective configuration</h2>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-left min-w-[720px]">
            <thead className="bg-[var(--color-surface-2)] text-[var(--color-text-muted)]">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Provider / model</th>
                <th className="p-2">Effort</th>
                <th className="p-2">Source</th>
                <th className="p-2">Limits</th>
              </tr>
            </thead>
            <tbody>
              {(effective.data ?? []).map(r => {
                const override = r.source === 'policy_approved'
                return (
                  <tr key={r.task} className="border-t border-[var(--color-border-subtle)]">
                    <td className="p-2 font-mono text-xs">{r.task}</td>
                    <td className="p-2">
                      {r.provider}/{r.model}
                    </td>
                    <td className="p-2">{r.effort || '—'}</td>
                    <td className="p-2">
                      <Badge variant={override ? 'admin' : 'muted'}>{override ? 'Approved policy' : r.source || 'Global'}</Badge>
                    </td>
                    <td className="p-2 font-mono text-xs">
                      tok {r.max_tokens || '—'} · {r.timeout_sec || '—'}s · ${r.max_cost_usd || '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="mb-2 font-medium">Task policies</h2>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-left min-w-[800px]">
            <thead className="bg-[var(--color-surface-2)]">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Current model</th>
                <th className="p-2">Previous</th>
                <th className="p-2">Approved by</th>
                <th className="p-2">Eval run</th>
                <th className="p-2" />
              </tr>
            </thead>
            <tbody>
              {(policies.data ?? []).map(r => {
                const d = detailByTask.get(r.task)
                return (
                  <tr key={r.task} className="border-t border-[var(--color-border-subtle)]">
                    <td className="p-2 font-mono text-xs">{r.task}</td>
                    <td className="p-2">{r.model || '—'}</td>
                    <td className="p-2 text-xs">{d?.previous_state ?? '—'}</td>
                    <td className="p-2 text-xs">{d?.approved_by || '—'}</td>
                    <td className="p-2 font-mono text-xs">{d?.eval_run_id?.slice(0, 8) ?? '—'}</td>
                    <td className="p-2">
                      {d?.rollback_available && (
                        <button type="button" className="text-xs text-[var(--color-admin)] underline" onClick={() => openRollback(r.task)}>
                          Rollback
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="mb-2 font-medium">Recommendations</h2>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-left">
            <thead className="bg-[var(--color-surface-2)]">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Outcome</th>
                <th className="p-2">Run</th>
                <th className="p-2">Reason</th>
                <th className="p-2" />
              </tr>
            </thead>
            <tbody>
              {(recs.data ?? []).map(r => (
                <tr key={r.id} className="border-t border-[var(--color-border-subtle)]">
                  <td className="p-2">{r.task}</td>
                  <td className="p-2">{r.outcome}</td>
                  <td className="p-2 font-mono text-xs">
                    {r.runner_type}/{r.run_purpose}
                  </td>
                  <td className="p-2 max-w-md truncate" title={r.reason}>
                    {r.reason}
                  </td>
                  <td className="p-2">
                    {r.approvable && (
                      <button type="button" className="text-xs underline" onClick={() => setRecReviewId(r.id)}>
                        Review
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h2 className="font-medium">Evaluations</h2>
        </div>
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]">
          <table className="w-full text-left">
            <thead className="bg-[var(--color-surface-2)]">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Dataset</th>
                <th className="p-2">Mode</th>
                <th className="p-2">Status</th>
                <th className="p-2">Spend (µUSD)</th>
                <th className="p-2" />
              </tr>
            </thead>
            <tbody>
              {(evals.data ?? []).map(r => (
                <tr key={r.id} className="border-t border-[var(--color-border-subtle)]">
                  <td className="p-2">{r.task}</td>
                  <td className="p-2">{r.dataset}</td>
                  <td className="p-2">
                    <Badge variant={r.runner_type === 'fake' ? 'warn' : 'success'}>
                      {r.runner_type}/{r.run_purpose}
                    </Badge>
                  </td>
                  <td className="p-2">{r.status}</td>
                  <td className="p-2 font-mono text-xs">{r.cost_micro}</td>
                  <td className="p-2 space-x-2">
                    <button type="button" className="text-xs underline" onClick={() => setEvalDetailId(r.id)}>
                      Detail
                    </button>
                    {r.status === 'running' && (
                      <button type="button" className="text-xs underline" onClick={() => cancelEval.mutate(r.id)}>
                        Cancel
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <div className="mb-2 flex items-center gap-3">
          <h2 className="font-medium">Catalog discovery</h2>
          <button
            type="button"
            className="rounded border border-[var(--color-border)] px-2 py-1 text-xs"
            disabled={refreshCatalog.isPending}
            onClick={() => refreshCatalog.mutate()}
          >
            Refresh LiteLLM staging
          </button>
        </div>
        <p className="mb-2 text-xs text-[var(--color-text-muted)]">
          Source: {catalog.data?.discovery_source ?? '—'} · last refresh: {catalog.data?.last_refresh_at || 'never'} · staged:{' '}
          {catalog.data?.candidates?.length ?? 0}
        </p>
      </section>

      <DialogRoot open={!!evalDetailId} onOpenChange={open => !open && setEvalDetailId(null)}>
        <DialogContent title="Evaluation run" className="max-w-lg max-h-[80vh] overflow-y-auto">
          {evalDetail.isLoading && <p className="text-sm">Loading…</p>}
          {evalDetail.data && (
            <pre className="text-xs overflow-x-auto whitespace-pre-wrap">{JSON.stringify(evalDetail.data, null, 2)}</pre>
          )}
        </DialogContent>
      </DialogRoot>

      <DialogRoot open={!!recReviewId} onOpenChange={open => !open && setRecReviewId(null)}>
        <DialogContent title="Review recommendation" description="Confirm before approving model policy.">
          {recDetail.data && (
            <div className="space-y-3 text-sm">
              <p>
                <span className="text-[var(--color-text-muted)]">Task:</span> {String(recDetail.data.task)}
              </p>
              <p className="text-xs text-[var(--color-text-muted)]">{String(recDetail.data.reason)}</p>
              <pre className="text-xs max-h-48 overflow-auto bg-[var(--color-surface-2)] p-2 rounded">
                {JSON.stringify(recDetail.data.metrics, null, 2)}
              </pre>
              {recReviewId && recDetail.data.deployable === true && (
                <Button
                  variant="primary"
                  loading={approve.isPending}
                  onClick={() =>
                    approve.mutate({
                      task: String(recDetail.data!.task),
                      recommendationId: recReviewId,
                    })
                  }
                >
                  Approve model policy
                </Button>
              )}
            </div>
          )}
        </DialogContent>
      </DialogRoot>

      <DialogRoot open={!!rollbackTask} onOpenChange={open => !open && setRollbackTask(null)}>
        <DialogContent title="Rollback task policy" description={rollbackMsg}>
          {rollbackTask && (
            <div className="flex gap-2 mt-4">
              <Button variant="secondary" onClick={() => setRollbackTask(null)}>
                Cancel
              </Button>
              <Button variant="primary" loading={rollback.isPending} onClick={() => rollback.mutate(rollbackTask)}>
                Confirm rollback
              </Button>
            </div>
          )}
        </DialogContent>
      </DialogRoot>
    </div>
  )
}
