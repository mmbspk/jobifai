import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { cn } from '../../lib'
import type { LLMConfig } from '../../types'

const TASKS = [
  { key: 'scoring', label: 'Suitability Scoring', hint: 'Once per job. Haiku recommended.' },
  { key: 'halal', label: 'Halal Filter', hint: 'No profile sent. Haiku recommended.' },
  { key: 'tailoring', label: 'Resume Tailoring', hint: 'Sonnet+ recommended.' },
  { key: 'cover_letter', label: 'Cover Letter', hint: 'Haiku or Sonnet.' },
  { key: 'form_filling', label: 'Form Q&A', hint: 'Per question. Haiku recommended.' },
  { key: 'questions', label: 'Interview Questions', hint: 'Batch Q&A.' },
] as const

export function AdminDefaultsPage() {
  const qc = useQueryClient()
  const { data: system } = useQuery({ queryKey: ['admin-system'], queryFn: adminApi.system.get })
  const { data: secrets } = useQuery({ queryKey: ['admin-system-secrets'], queryFn: adminApi.systemSecrets.get })
  const [llm, setLlm] = useState<LLMConfig>({})
  const [apiKey, setApiKey] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (system?.llm) setLlm(system.llm)
  }, [system])

  const saveSystem = useMutation({
    mutationFn: async () => {
      const base = system ?? {}
      await adminApi.system.set({ ...base, llm })
      if (apiKey.trim()) {
        await adminApi.systemSecrets.setApiKey(apiKey.trim())
        setApiKey('')
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-system'] })
      qc.invalidateQueries({ queryKey: ['admin-system-secrets'] })
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
    <div className="space-y-4">
      <p className="text-sm text-[var(--color-text-dim)]">
        Deployment-wide LLM defaults. Every account inherits these unless an admin sets per-user overrides on the Users tab.
      </p>

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-4">
        <div className="text-xs font-semibold uppercase tracking-wider text-[var(--color-text-muted)]">Default LLM</div>
        <div className="flex flex-wrap gap-4">
          <label className="text-sm space-y-1">
            <span className="text-[var(--color-text-muted)]">Provider</span>
            <select value={llm.provider ?? 'claude'} onChange={e => setLlm({ ...llm, provider: e.target.value })}
              className="block bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm">
              <option value="claude">Claude</option>
              <option value="openai">OpenAI</option>
              <option value="ollama">Ollama</option>
            </select>
          </label>
          <label className="text-sm space-y-1 flex-1 min-w-[200px]">
            <span className="text-[var(--color-text-muted)]">Default model</span>
            <input value={llm.model ?? ''} onChange={e => setLlm({ ...llm, model: e.target.value })}
              placeholder="claude-sonnet-4-6"
              className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm" />
          </label>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={llm.use_proxy ?? false} onChange={e => setLlm({ ...llm, use_proxy: e.target.checked })} className="accent-violet-500" />
          Route through proxy
        </label>
        {llm.use_proxy && (
          <input value={llm.proxy_url ?? ''} onChange={e => setLlm({ ...llm, proxy_url: e.target.value })}
            placeholder="Proxy URL"
            className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm" />
        )}
      </div>

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-3">
        <div className="text-xs font-semibold uppercase tracking-wider text-[var(--color-text-muted)]">Default task models</div>
        {TASKS.map(t => (
          <div key={t.key} className="flex items-center justify-between gap-4">
            <div>
              <div className="text-sm">{t.label}</div>
              <div className="text-xs text-[var(--color-text-dim)]">{t.hint}</div>
            </div>
            <input
              value={llm.task_models?.[t.key]?.model ?? ''}
              onChange={e => setTaskModel(t.key, e.target.value)}
              placeholder={llm.model ?? 'inherit default'}
              className="w-48 bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm"
            />
          </div>
        ))}
      </div>

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-2">
        <div className="text-xs font-semibold uppercase tracking-wider text-[var(--color-text-muted)]">Default API key</div>
        <p className="text-xs text-[var(--color-text-dim)]">
          {secrets?.has_default_api_key ? 'A default key is configured (shown masked).' : 'No default key yet — all users need a per-user key or set one here.'}
        </p>
        <input
          type="password"
          value={apiKey}
          onChange={e => setApiKey(e.target.value)}
          placeholder={secrets?.has_default_api_key ? 'Enter new key to replace' : 'sk-…'}
          className="w-full max-w-md bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm"
        />
      </div>

      <Button variant="primary" loading={saveSystem.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => saveSystem.mutate()}>
        {saved ? 'Saved' : 'Save defaults'}
      </Button>
      {saveSystem.isError && (
        <div className={cn('text-xs text-red-400')}>{(saveSystem.error as Error).message}</div>
      )}
    </div>
  )
}
