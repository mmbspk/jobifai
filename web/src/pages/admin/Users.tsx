import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { cn } from '../../lib'
import type { AdminUserRow, LLMOverrides } from '../../types'

const TASKS = ['scoring', 'halal', 'tailoring', 'cover_letter', 'form_filling', 'questions'] as const

export function AdminUsersPage() {
  const qc = useQueryClient()
  const { data: users = [] } = useQuery({ queryKey: ['admin-users'], queryFn: adminApi.users.list })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const { data: detail } = useQuery({
    queryKey: ['admin-user', selectedId],
    queryFn: () => adminApi.users.get(selectedId!),
    enabled: !!selectedId,
  })
  const [overrides, setOverrides] = useState<LLMOverrides>({})
  const [userApiKey, setUserApiKey] = useState('')

  useEffect(() => {
    if (detail) setOverrides(detail.llm_overrides ?? {})
  }, [detail])

  const selectUser = (u: AdminUserRow) => {
    setSelectedId(u.id)
    setUserApiKey('')
  }

  const save = useMutation({
    mutationFn: async () => {
      if (!selectedId) return
      await adminApi.users.update(selectedId, { llm_overrides: overrides })
      if (userApiKey.trim()) {
        await adminApi.users.setApiKey(selectedId, userApiKey.trim())
        setUserApiKey('')
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
    },
  })

  const toggleAdmin = useMutation({
    mutationFn: (next: boolean) => adminApi.users.update(selectedId!, { is_admin: next }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
    },
  })

  return (
    <div className="grid lg:grid-cols-2 gap-6">
      <div className="rounded-xl border border-[var(--color-border)] overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--color-border)] text-xs font-semibold uppercase text-[var(--color-text-muted)]">Accounts</div>
        <ul className="divide-y divide-[var(--color-border)] max-h-[480px] overflow-y-auto">
          {users.map(u => (
            <li key={u.id}>
              <button
                type="button"
                onClick={() => selectUser(u)}
                className={cn(
                  'w-full text-left px-4 py-3 hover:bg-[var(--color-surface-2)] transition-colors',
                  selectedId === u.id && 'bg-[var(--color-surface-2)]',
                )}
              >
                <div className="text-sm font-medium text-[var(--color-text)]">{u.display_name || u.email}</div>
                <div className="text-xs text-[var(--color-text-dim)]">{u.email}</div>
                <div className="flex gap-2 mt-1">
                  {u.is_admin && <span className="text-[10px] uppercase tracking-wide text-amber-400">Admin</span>}
                  {u.has_api_key && <span className="text-[10px] uppercase tracking-wide text-violet-400">Own API key</span>}
                </div>
              </button>
            </li>
          ))}
        </ul>
      </div>

      <div className="rounded-xl border border-[var(--color-border)] p-4 space-y-4 min-h-[200px]">
        {!selectedId || !detail ? (
          <p className="text-sm text-[var(--color-text-dim)]">Select an account to edit admin access and LLM overrides.</p>
        ) : (
          <>
            <div>
              <h2 className="text-sm font-semibold">{detail.email}</h2>
              <label className="flex items-center gap-2 mt-3 text-sm">
                <input
                  type="checkbox"
                  checked={detail.is_admin}
                  onChange={e => toggleAdmin.mutate(e.target.checked)}
                  className="accent-amber-500"
                />
                Administrator
              </label>
            </div>

            <div className="space-y-2">
              <div className="text-xs font-semibold uppercase text-[var(--color-text-muted)]">LLM overrides (optional)</div>
              <input
                value={overrides.model ?? ''}
                onChange={e => setOverrides({ ...overrides, model: e.target.value || undefined })}
                placeholder="Override default model"
                className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm"
              />
              {TASKS.map(task => (
                <input
                  key={task}
                  value={overrides.task_models?.[task]?.model ?? ''}
                  onChange={e => setOverrides({
                    ...overrides,
                    task_models: {
                      ...overrides.task_models,
                      [task]: { model: e.target.value || undefined },
                    },
                  })}
                  placeholder={`Task: ${task} (inherit if empty)`}
                  className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm"
                />
              ))}
            </div>

            <div className="space-y-2">
              <div className="text-xs font-semibold uppercase text-[var(--color-text-muted)]">Per-user API key</div>
              <p className="text-xs text-[var(--color-text-dim)]">
                {detail.has_api_key ? 'This user has their own key (overrides deployment default).' : 'Uses deployment default key.'}
              </p>
              <input
                type="password"
                value={userApiKey}
                onChange={e => setUserApiKey(e.target.value)}
                placeholder="Set or replace user key"
                className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm"
              />
              {detail.has_api_key && (
                <button type="button" className="text-xs text-[var(--color-danger)]" onClick={() => adminApi.users.deleteApiKey(selectedId).then(() => qc.invalidateQueries({ queryKey: ['admin-user', selectedId] }))}>
                  Remove user key (fall back to default)
                </button>
              )}
            </div>

            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>Save user settings</Button>
          </>
        )}
      </div>
    </div>
  )
}
