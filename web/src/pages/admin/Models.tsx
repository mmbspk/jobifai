import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost } from '../../api/client'

type PolicyRow = { task: string; provider: string; model: string; mode: string; effort: string; state: string }
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
  const policies = useQuery({
    queryKey: ['admin', 'models', 'policies'],
    queryFn: () => apiGet<PolicyRow[]>('/api/admin/models/policies'),
  })
  const effective = useQuery({
    queryKey: ['admin', 'models', 'effective'],
    queryFn: () => apiGet<EffectiveRow[]>('/api/admin/models/effective'),
  })
  const evals = useQuery({
    queryKey: ['admin', 'models', 'evals'],
    queryFn: () => apiGet<EvalRow[]>('/api/admin/models/evals'),
  })
  const recs = useQuery({
    queryKey: ['admin', 'models', 'recommendations'],
    queryFn: () => apiGet<RecRow[]>('/api/admin/models/recommendations'),
  })
  const catalog = useQuery({
    queryKey: ['admin', 'models', 'catalog'],
    queryFn: () => apiGet<{ discovery_source: string; last_refresh_at: string; candidates: unknown[] }>('/api/admin/models/catalog'),
  })
  const refreshCatalog = useMutation({
    mutationFn: () => apiPost('/api/admin/models/catalog/refresh', {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'models', 'catalog'] }),
  })
  const cancelEval = useMutation({
    mutationFn: (id: string) => apiPost(`/api/admin/models/evals/${id}/cancel`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'models', 'evals'] }),
  })

  return (
    <div className="space-y-8 text-sm">
      <section>
        <h2 className="mb-2 font-medium">Effective configuration</h2>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-left">
            <thead className="bg-muted/40">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Provider / model</th>
                <th className="p-2">Effort</th>
                <th className="p-2">Source</th>
                <th className="p-2">Limits</th>
              </tr>
            </thead>
            <tbody>
              {(effective.data ?? []).map((r) => (
                <tr key={r.task} className="border-t border-border">
                  <td className="p-2 font-mono text-xs">{r.task}</td>
                  <td className="p-2">
                    {r.provider}/{r.model}
                  </td>
                  <td className="p-2">{r.effort || '—'}</td>
                  <td className="p-2">{r.source}</td>
                  <td className="p-2 font-mono text-xs">
                    tok {r.max_tokens || '—'} · {r.timeout_sec || '—'}s · ${r.max_cost_usd || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="mb-2 font-medium">Task policies</h2>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-left">
            <thead className="bg-muted/40">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Model</th>
                <th className="p-2">Effort</th>
                <th className="p-2">State</th>
              </tr>
            </thead>
            <tbody>
              {(policies.data ?? []).map((r) => (
                <tr key={r.task} className="border-t border-border">
                  <td className="p-2 font-mono text-xs">{r.task}</td>
                  <td className="p-2">{r.model || '—'}</td>
                  <td className="p-2">{r.effort || '—'}</td>
                  <td className="p-2">{r.state}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h2 className="font-medium">Evaluations</h2>
          <span className="text-xs text-muted-foreground">Real runs default; fake runs show badge and cannot approve policy</span>
        </div>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-left">
            <thead className="bg-muted/40">
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
              {(evals.data ?? []).map((r) => (
                <tr key={r.id} className="border-t border-border">
                  <td className="p-2">{r.task}</td>
                  <td className="p-2">{r.dataset}</td>
                  <td className="p-2">
                    <span
                      className={
                        r.runner_type === 'fake'
                          ? 'rounded bg-amber-500/20 px-1.5 py-0.5 text-xs text-amber-800 dark:text-amber-200'
                          : 'rounded bg-emerald-500/20 px-1.5 py-0.5 text-xs'
                      }
                    >
                      {r.runner_type}/{r.run_purpose}
                    </span>
                  </td>
                  <td className="p-2">{r.status}</td>
                  <td className="p-2 font-mono text-xs">{r.cost_micro}</td>
                  <td className="p-2">
                    {r.status === 'running' && (
                      <button
                        type="button"
                        className="text-xs underline"
                        onClick={() => cancelEval.mutate(r.id)}
                      >
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
        <h2 className="mb-2 font-medium">Recommendations</h2>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-left">
            <thead className="bg-muted/40">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Outcome</th>
                <th className="p-2">Run</th>
                <th className="p-2">Approvable</th>
                <th className="p-2">Reason</th>
              </tr>
            </thead>
            <tbody>
              {(recs.data ?? []).map((r) => (
                <tr key={r.id} className="border-t border-border">
                  <td className="p-2">{r.task}</td>
                  <td className="p-2">{r.outcome}</td>
                  <td className="p-2 font-mono text-xs">
                    {r.runner_type}/{r.run_purpose}
                  </td>
                  <td className="p-2">{r.approvable ? 'yes' : 'no'}</td>
                  <td className="p-2 max-w-md truncate" title={r.reason}>
                    {r.reason}
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
            className="rounded border border-border px-2 py-1 text-xs"
            disabled={refreshCatalog.isPending}
            onClick={() => refreshCatalog.mutate()}
          >
            Refresh LiteLLM staging
          </button>
        </div>
        <p className="mb-2 text-xs text-muted-foreground">
          Source: {catalog.data?.discovery_source ?? '—'} · last refresh: {catalog.data?.last_refresh_at || 'never'} · staged:{' '}
          {catalog.data?.candidates?.length ?? 0}
        </p>
      </section>
    </div>
  )
}
