import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check, Loader2 } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsSection, SettingsSelect } from '../../components/settings/settings-ui'
import { MaskedSecretField } from '../../components/settings/MaskedSecretField'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
import { cn } from '../../lib'
import type { LLMConfig } from '../../types'

export function AdminAIProviderPage() {
  const qc = useQueryClient()
  const { data: system } = useQuery({ queryKey: ['admin-system'], queryFn: adminApi.system.get })
  const { data: secrets } = useQuery({ queryKey: ['admin-system-secrets'], queryFn: adminApi.systemSecrets.get })
  const [llm, setLlm] = useState<LLMConfig>({})
  const [saved, setSaved] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{
    success: boolean
    latency_ms: number
    provider: string
    model: string
    error?: string
  } | null>(null)

  useEffect(() => {
    if (system?.llm) setLlm(system.llm)
  }, [system])

  const saveSystem = useMutation({
    mutationFn: async () => {
      const base = system ?? {}
      await adminApi.system.set({ ...base, llm })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-system'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await adminApi.aiProvider.test()
      setTestResult(res)
    } catch (e) {
      setTestResult({ success: false, latency_ms: 0, provider: '', model: '', error: String(e) })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="AI Provider"
        description="Configure the default LLM provider and API key. All accounts use this unless a tester has configured a personal provider."
      />

      <SettingsSection title="Provider">
        <div className="flex flex-wrap gap-4">
          <SettingsField label="Provider" layout="column">
            <SettingsSelect value={llm.provider ?? 'claude'} onChange={e => setLlm({ ...llm, provider: e.target.value })}>
              <option value="claude">Claude (Anthropic)</option>
              <option value="openai">OpenAI</option>
              <option value="gemini">Gemini (Google)</option>
              <option value="ollama">Ollama (local)</option>
            </SettingsSelect>
          </SettingsField>
          <SettingsField label="Default model" layout="column">
            <input
              value={llm.model ?? ''}
              onChange={e => setLlm({ ...llm, model: e.target.value })}
              placeholder="claude-sonnet-4-5"
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
        <Button variant="primary" loading={saveSystem.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => saveSystem.mutate()}>
          {saved ? 'Saved' : 'Save provider settings'}
        </Button>
        {saveSystem.isError && (
          <p className="text-xs text-[var(--color-danger)]">{(saveSystem.error as Error).message}</p>
        )}
      </SettingsSection>

      <SettingsSection title="API key" description="Stored encrypted. Used for all accounts that don't have a personal key.">
        <MaskedSecretField
          configured={!!secrets?.has_default_api_key}
          label="API key"
          helper={secrets?.has_default_api_key ? 'A deployment API key is configured.' : 'No default key — tester accounts must supply their own.'}
          onSave={v => adminApi.systemSecrets.setApiKey(v).then(() => qc.invalidateQueries({ queryKey: ['admin-system-secrets'] }))}
          onDelete={() => adminApi.systemSecrets.deleteApiKey().then(() => qc.invalidateQueries({ queryKey: ['admin-system-secrets'] }))}
        />
      </SettingsSection>

      <SettingsSection title="Connection test" description="Verify the current provider and API key work by sending a test request.">
        <div className="flex items-center gap-3">
          <Button variant="secondary" onClick={handleTest} disabled={testing}>
            {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Test connection'}
          </Button>
          {testResult && (
            <span className={cn(
              'text-sm',
              testResult.success ? 'text-[var(--color-success)]' : 'text-[var(--color-danger)]',
            )}>
              {testResult.success
                ? `OK · ${testResult.provider}/${testResult.model} · ${testResult.latency_ms}ms`
                : (testResult.error ?? 'Connection failed')}
            </span>
          )}
        </div>
      </SettingsSection>
    </div>
  )
}
