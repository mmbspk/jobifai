import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'

type PolicyRow = { task: string; provider: string; model: string; mode: string; effort: string; state: string }
type EvalRow = { id: string; task: string; dataset: string; status: string; cost_micro: number; created: string }

export function AdminModelsPage() {
  const policies = useQuery({
    queryKey: ['admin', 'models', 'policies'],
    queryFn: () => apiGet<PolicyRow[]>('/api/admin/models/policies'),
  })
  const evals = useQuery({
    queryKey: ['admin', 'models', 'evals'],
    queryFn: () => apiGet<EvalRow[]>('/api/admin/models/evals'),
  })

  return (
    <div className="space-y-8 text-sm">
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
        <h2 className="mb-2 font-medium">Evaluations</h2>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-left">
            <thead className="bg-muted/40">
              <tr>
                <th className="p-2">Task</th>
                <th className="p-2">Dataset</th>
                <th className="p-2">Status</th>
                <th className="p-2">Spend (µUSD)</th>
              </tr>
            </thead>
            <tbody>
              {(evals.data ?? []).map((r) => (
                <tr key={r.id} className="border-t border-border">
                  <td className="p-2">{r.task}</td>
                  <td className="p-2">{r.dataset}</td>
                  <td className="p-2">{r.status}</td>
                  <td className="p-2 font-mono text-xs">{r.cost_micro}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
