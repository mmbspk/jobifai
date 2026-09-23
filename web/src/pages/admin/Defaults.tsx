import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsSection, SettingsSelect } from '../../components/settings/settings-ui'
import { MaskedSecretField } from '../../components/settings/MaskedSecretField'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
import { cn } from '../../lib'
import type { LLMConfig } from '../../types'

const TASKS = [
  { key: 'scoring', label: 'Suitability scoring', hint: 'Once per job. Haiku recommended.' },
  { key: 'halal', label: 'Halal filter', hint: 'No profile sent. Haiku recommended.' },
  { key: 'tailoring', label: 'Resume tailoring', hint: 'Sonnet+ recommended.' },
  { key: 'cover_letter', label: 'Cover letter', hint: 'Haiku or Sonnet.' },
  { key: 'form_filling', label: 'Form Q&A', hint: 'Per question. Haiku recommended.' },
  { key: 'questions', label: 'Interview questions', hint: 'Batch Q&A.' },
] as const

export function AdminDefaultsPage() {
  const qc = useQueryClient()
  const { data: system } = useQuery({ queryKey: ['admin-system'], queryFn: adminApi.system.get })
  const { data: secrets } = useQuery({ queryKey: ['admin-system-secrets'], queryFn: adminApi.systemSecrets.get })
  const [llm, setLlm] = useState<LLMConfig>({})
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (system?.llm) setLlm(system.llm)
  }, [system])

  const saveSystem = useMutation({
    mutationFn: async () => {
      const base = system ?? {}
      await adminApi.system.set({ ...base, llm })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-system'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  function setTaskModel(task: string, model: string) {
    setLlm(prev => ({
      ...prev,
      task_models: { ...prev.task_models, [task]: { ...prev.task_models?.[task], model: model || undefined } },
    }))
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Defaults"
        description="Deployment-wide LLM settings. Accounts inherit these unless overridden on the Users tab."
      />

      <SettingsSection title="Default LLM">
        <div className="flex flex-wrap gap-4">
          <SettingsField label="Provider" layout="column">
            <SettingsSelect value={llm.provider ?? 'claude'} onChange={e => setLlm({ ...llm, provider: e.target.value })}>
              <option value="claude">Claude</option>
              <option value="openai">OpenAI</option>
              <option value="ollama">Ollama</option>
            </SettingsSelect>
          </SettingsField>
          <SettingsField label="Default model" layout="column">
            <input
              value={llm.model ?? ''}
              onChange={e => setLlm({ ...llm, model: e.target.value })}
              placeholder="claude-sonnet-4-6"
              className={cn(inputClassName, 'min-w-[200px]')}
            />
          </SettingsField>
        </div>
        <Switch
          label="Route through proxy"
          checked={llm.use_proxy ?? false}
          onCheckedChange={v => setLlm({ ...llm, use_proxy: v })}
        />
        {llm.use_proxy && (
          <input
            value={llm.proxy_url ?? ''}
            onChange={e => setLlm({ ...llm, proxy_url: e.target.value })}
            placeholder="Proxy URL"
            className={inputClassName}
          />
        )}
      </SettingsSection>

      <SettingsSection title="Default task models" description="Leave blank to inherit the default model above.">
        {TASKS.map(t => (
          <SettingsField key={t.key} label={t.label} sub={t.hint} layout="column">
            <input
              value={llm.task_models?.[t.key]?.model ?? ''}
              onChange={e => setTaskModel(t.key, e.target.value)}
              placeholder={llm.model ?? 'inherit default'}
              className={cn(inputClassName, 'max-w-md')}
            />
          </SettingsField>
        ))}
      </SettingsSection>

      <SettingsSection title="Default API key" description="Used when a user has not set their own key on Platforms.">
        <MaskedSecretField
          configured={!!secrets?.has_default_api_key}
          label="API key"
          helper={secrets?.has_default_api_key ? 'A deployment default is configured.' : 'No default key — each user must supply one.'}
          onSave={v => adminApi.systemSecrets.setApiKey(v).then(() => qc.invalidateQueries({ queryKey: ['admin-system-secrets'] }))}
          onDelete={() => adminApi.systemSecrets.deleteApiKey().then(() => qc.invalidateQueries({ queryKey: ['admin-system-secrets'] }))}
        />
      </SettingsSection>

      <Button variant="primary" fullWidth loading={saveSystem.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => saveSystem.mutate()}>
        {saved ? 'Saved' : 'Save changes'}
      </Button>
      {saveSystem.isError && (
        <p className="text-xs text-[var(--color-danger)] text-center">{(saveSystem.error as Error).message}</p>
      )}
    </div>
  )
}
