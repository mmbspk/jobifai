import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection } from '../../components/settings/settings-ui'
import { MaskedSecretField } from '../../components/settings/MaskedSecretField'
import { Badge } from '../../components/ui/badge'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
import { cn } from '../../lib'
import type { AdminUserRow, LLMOverrides, QuotaUserOverrides } from '../../types'

const TASKS = [
  { key: 'scoring', label: 'Suitability scoring' },
  { key: 'halal', label: 'Halal filter' },
  { key: 'tailoring', label: 'Resume tailoring' },
  { key: 'cover_letter', label: 'Cover letter' },
  { key: 'form_filling', label: 'Form Q&A' },
  { key: 'questions', label: 'Interview questions' },
] as const

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
  const [quotaOverrides, setQuotaOverrides] = useState<QuotaUserOverrides>({})
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (detail) {
      setOverrides(detail.llm_overrides ?? {})
      setQuotaOverrides(detail.quota_overrides ?? {})
    }
  }, [detail])

  const selectUser = (u: AdminUserRow) => {
    setSelectedId(u.id)
    setSaved(false)
  }

  const save = useMutation({
    mutationFn: async () => {
      if (!selectedId) return
      await adminApi.users.update(selectedId, { llm_overrides: overrides, quota_overrides: quotaOverrides })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  const toggleAdmin = useMutation({
    mutationFn: (next: boolean) => adminApi.users.update(selectedId!, { is_admin: next }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
    },
  })

  const pruneE2E = useMutation({
    mutationFn: () => adminApi.users.pruneE2E(),
    onSuccess: () => {
      setSelectedId(null)
      qc.invalidateQueries({ queryKey: ['admin-users'] })
    },
  })

  const deleteUser = useMutation({
    mutationFn: (userId: string) => adminApi.users.delete(userId),
    onSuccess: () => {
      setSelectedId(null)
      qc.invalidateQueries({ queryKey: ['admin-users'] })
    },
  })

  const e2eCount = users.filter(u => u.email.endsWith('@e2e.test')).length

  return (
    <div className="space-y-6">
      <PageHeader
        title="Users"
        description="Grant admin access, credit enforcement, LLM overrides, and API keys."
      />

      {e2eCount > 0 && (
        <div className="rounded-[var(--radius-lg)] border border-amber-500/30 bg-amber-500/5 px-4 py-3 flex flex-col sm:flex-row sm:items-center gap-3">
          <p className="text-sm text-[var(--color-text-muted)] flex-1">
            {e2eCount} Playwright test account{e2eCount === 1 ? '' : 's'} (@e2e.test) — usually created when E2E ran against the dev server instead of the isolated test DB.
          </p>
          <Button variant="secondary" loading={pruneE2E.isPending} onClick={() => pruneE2E.mutate()}>
            Remove test accounts
          </Button>
        </div>
      )}

      <div className="grid lg:grid-cols-2 gap-6">
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden shadow-[var(--shadow-sm)]">
          <div className="px-4 py-3 border-b border-[var(--color-border-subtle)]">
            <h2 className="text-sm font-semibold text-[var(--color-text)]">Accounts</h2>
          </div>
          <ul className="divide-y divide-[var(--color-border-subtle)] max-h-[480px] overflow-y-auto">
            {users.map(u => (
              <li key={u.id}>
                <button
                  type="button"
                  onClick={() => selectUser(u)}
                  className={cn(
                    'w-full text-left px-4 py-3 hover:bg-[var(--color-surface-2)] transition-colors min-h-[44px]',
                    selectedId === u.id && 'bg-[var(--color-admin-soft)]/40 border-l-2 border-l-[var(--color-admin)]',
                  )}
                >
                  <div className="text-sm font-medium text-[var(--color-text)]">{u.display_name || u.email}</div>
                  <div className="text-xs text-[var(--color-text-dim)]">{u.email}</div>
                  <div className="flex flex-wrap gap-1.5 mt-1.5">
                    {u.is_admin && <Badge variant="admin">Admin</Badge>}
                    {u.has_api_key && <Badge variant="accent">Own API key</Badge>}
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </section>

        <div className="min-h-[200px]">
          {!selectedId || !detail ? (
            <p className="text-sm text-[var(--color-text-dim)] px-1">Select an account to edit access and LLM overrides.</p>
          ) : (
            <div className="space-y-6">
              <SettingsSection title={detail.display_name || detail.email} description={detail.email}>
                <Switch
                  label="Administrator"
                  helper="Can open the Admin area and change deployment defaults."
                  checked={detail.is_admin}
                  onCheckedChange={v => toggleAdmin.mutate(v)}
                />
              </SettingsSection>

              <SettingsSection title="Credits & enforcement" description="Per-account quota overrides. Admins are always unlimited.">
                <Switch
                  label="Enforce credit limits"
                  helper="When off, this account can use AI without credit deductions (not recommended for production users)."
                  checked={quotaOverrides.enforcement_enabled ?? true}
                  onCheckedChange={v => setQuotaOverrides({ ...quotaOverrides, enforcement_enabled: v })}
                />
                <SettingsField label="Trial credits override" layout="column" sub="Only applies while account is on trial">
                  <SettingsNumberInput
                    value={quotaOverrides.trial_credits ?? 0}
                    min={0}
                    className="w-full max-w-[140px]"
                    onChange={v =>
                      setQuotaOverrides({ ...quotaOverrides, trial_credits: v > 0 ? v : undefined })
                    }
                  />
                </SettingsField>
                <SettingsField label="Period allowance override" layout="column" sub="Credits per billing period (starter/pro)">
                  <SettingsNumberInput
                    value={Number(quotaOverrides.period_allowance_credits ?? 0)}
                    min={0}
                    className="w-full max-w-[140px]"
                    onChange={v =>
                      setQuotaOverrides({
                        ...quotaOverrides,
                        period_allowance_credits: v > 0 ? v : undefined,
                      })
                    }
                  />
                </SettingsField>
              </SettingsSection>

              <SettingsSection title="LLM overrides" description="Optional — leave blank to inherit deployment defaults.">
                <SettingsField label="Default model override" layout="column">
                  <input
                    value={overrides.model ?? ''}
                    onChange={e => setOverrides({ ...overrides, model: e.target.value || undefined })}
                    placeholder="Inherit deployment default"
                    className={inputClassName}
                  />
                </SettingsField>
                {TASKS.map(task => (
                  <SettingsField key={task.key} label={task.label} layout="column">
                    <input
                      value={overrides.task_models?.[task.key]?.model ?? ''}
                      onChange={e => setOverrides({
                        ...overrides,
                        task_models: {
                          ...overrides.task_models,
                          [task.key]: { model: e.target.value || undefined },
                        },
                      })}
                      placeholder="Inherit if empty"
                      className={inputClassName}
                    />
                  </SettingsField>
                ))}
              </SettingsSection>

              <SettingsSection title="Per-user API key">
                <MaskedSecretField
                  configured={detail.has_api_key}
                  label="API key"
                  helper={detail.has_api_key ? 'Overrides the deployment default for this account.' : 'Uses the deployment default key from Defaults.'}
                  onSave={v => adminApi.users.setApiKey(selectedId, v).then(() => {
                    qc.invalidateQueries({ queryKey: ['admin-users'] })
                    qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
                  })}
                  onDelete={() => adminApi.users.deleteApiKey(selectedId).then(() => {
                    qc.invalidateQueries({ queryKey: ['admin-users'] })
                    qc.invalidateQueries({ queryKey: ['admin-user', selectedId] })
                  })}
                />
              </SettingsSection>

              <Button variant="primary" fullWidth loading={save.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => save.mutate()}>
                {saved ? 'Saved' : 'Save changes'}
              </Button>

              {detail.id !== '__default__' && (
                <Button
                  variant="secondary"
                  fullWidth
                  loading={deleteUser.isPending}
                  className="text-[var(--color-danger)] border-[var(--color-danger)]/30 hover:bg-red-500/10"
                  onClick={() => {
                    if (window.confirm(`Delete ${detail.email} and all of their data?`)) {
                      deleteUser.mutate(detail.id)
                    }
                  }}
                >
                  Delete account
                </Button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
