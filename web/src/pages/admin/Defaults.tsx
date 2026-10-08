import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import type { EmailSettingsRequest } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsSection, SettingsSelect } from '../../components/settings/settings-ui'
import { MaskedSecretField } from '../../components/settings/MaskedSecretField'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
import { cn } from '../../lib'
import type { AppSettings, LLMConfig } from '../../types'

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
  const { data: appData } = useQuery({ queryKey: ['admin-app-settings'], queryFn: adminApi.app.settings.get })
  const { data: system } = useQuery({ queryKey: ['admin-system'], queryFn: adminApi.system.get })
  const { data: secrets } = useQuery({ queryKey: ['admin-system-secrets'], queryFn: adminApi.systemSecrets.get })
  const { data: emailData } = useQuery({ queryKey: ['admin-email-settings'], queryFn: adminApi.email.settings.get })
  const [appCfg, setAppCfg] = useState<AppSettings>({})
  const [appSaved, setAppSaved] = useState(false)
  const [appError, setAppError] = useState<string | null>(null)
  const [llm, setLlm] = useState<LLMConfig>({})
  const [saved, setSaved] = useState(false)
  const [emailCfg, setEmailCfg] = useState<EmailSettingsRequest>({})
  const [emailSaved, setEmailSaved] = useState(false)
  const [testTo, setTestTo] = useState('')
  const [testMsg, setTestMsg] = useState<string | null>(null)
  const [testError, setTestError] = useState<string | null>(null)

  useEffect(() => {
    if (appData) setAppCfg({ public_app_url: appData.public_app_url ?? '' })
  }, [appData])

  useEffect(() => {
    if (system?.llm) setLlm(system.llm)
  }, [system])

  useEffect(() => {
    if (emailData) {
      setEmailCfg({
        email_provider: emailData.email_provider ?? '',
        smtp_host: emailData.smtp_host ?? '',
        smtp_port: emailData.smtp_port ?? 587,
        smtp_user: emailData.smtp_user ?? '',
        email_from: emailData.email_from ?? '',
      })
    }
  }, [emailData])

  const saveApp = useMutation({
    mutationFn: () => adminApi.app.settings.set(appCfg),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-app-settings'] })
      setAppSaved(true)
      setAppError(null)
      setTimeout(() => setAppSaved(false), 2000)
    },
    onError: (e: Error) => {
      setAppError(e.message)
    },
  })

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

  const saveEmail = useMutation({
    mutationFn: () => adminApi.email.settings.set(emailCfg),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-email-settings'] })
      setEmailSaved(true)
      setTimeout(() => setEmailSaved(false), 2000)
    },
  })

  const sendTestEmail = useMutation({
    mutationFn: () => adminApi.email.test(testTo),
    onSuccess: (res) => {
      setTestMsg(res.message)
      setTestError(null)
    },
    onError: (e: Error) => {
      setTestError(e.message)
      setTestMsg(null)
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

      <SettingsSection title="Application">
        <SettingsField layout="column" label="Public application URL">
          <input
            className={inputClassName}
            value={appCfg.public_app_url ?? ''}
            onChange={e => setAppCfg({ ...appCfg, public_app_url: e.target.value })}
            placeholder="https://jobifai.com.au"
          />
          <p className="text-sm text-muted-foreground mt-1">
            The public browser URL for Jobifai (e.g. <code>https://jobifai.com.au</code>). Used in
            verification, password-reset and other externally generated links. Use the root origin
            only — no path, query string or fragment. Leaving this empty allows the{' '}
            <code>APP_BASE_URL</code> deployment value or browser-origin local-development fallback
            to be used; clearing it does not permanently disable the deployment bootstrap value.
          </p>
        </SettingsField>
        <div className="flex items-center gap-3 mt-2">
          <Button onClick={() => saveApp.mutate()} disabled={saveApp.isPending}>
            {appSaved ? <Check className="h-4 w-4 text-green-500" /> : null}
            {appSaved ? 'Saved' : 'Save application settings'}
          </Button>
        </div>
        {appError && <p className="text-sm text-destructive mt-1">{appError}</p>}
      </SettingsSection>

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

      <SettingsSection title="Transactional email" description="SMTP settings for verification emails. Leave blank to disable email sending.">
        <div className="flex flex-wrap gap-4">
          <SettingsField label="SMTP host" layout="column">
            <input
              value={emailCfg.smtp_host ?? ''}
              onChange={e => setEmailCfg({ ...emailCfg, smtp_host: e.target.value })}
              placeholder="smtp.resend.com"
              className={cn(inputClassName, 'min-w-[200px]')}
            />
          </SettingsField>
          <SettingsField label="SMTP port" layout="column">
            <input
              type="number"
              value={emailCfg.smtp_port ?? 587}
              onChange={e => setEmailCfg({ ...emailCfg, smtp_port: Number.parseInt(e.target.value, 10) || 587 })}
              className={cn(inputClassName, 'w-24')}
            />
          </SettingsField>
          <SettingsField label="SMTP username" layout="column">
            <input
              value={emailCfg.smtp_user ?? ''}
              onChange={e => setEmailCfg({ ...emailCfg, smtp_user: e.target.value })}
              placeholder="resend"
              className={cn(inputClassName, 'min-w-[160px]')}
            />
          </SettingsField>
          <SettingsField label="From address" layout="column">
            <input
              value={emailCfg.email_from ?? ''}
              onChange={e => setEmailCfg({ ...emailCfg, email_from: e.target.value })}
              placeholder="Jobifai <noreply@example.com>"
              className={cn(inputClassName, 'min-w-[240px]')}
            />
          </SettingsField>
        </div>
        <MaskedSecretField
          configured={!!emailData?.has_smtp_pass}
          label="SMTP password"
          helper={emailData?.has_smtp_pass ? 'A password is configured.' : 'No password set.'}
          onSave={v =>
            adminApi.email.settings.set({ ...emailCfg, smtp_pass: v }).then(() =>
              qc.invalidateQueries({ queryKey: ['admin-email-settings'] }),
            )
          }
          onDelete={() =>
            adminApi.email.settings.deleteSmtpPass().then(() =>
              qc.invalidateQueries({ queryKey: ['admin-email-settings'] }),
            )
          }
        />
        <Button
          variant="primary"
          loading={saveEmail.isPending}
          leftIcon={emailSaved ? <Check size={14} /> : undefined}
          onClick={() => saveEmail.mutate()}
        >
          {emailSaved ? 'Saved' : 'Save email settings'}
        </Button>
        {saveEmail.isError && (
          <p className="text-xs text-[var(--color-danger)]">{(saveEmail.error as Error).message}</p>
        )}
        <SettingsField label="Send test email" sub="Verify the configuration is working." layout="column">
          <div className="flex gap-2">
            <input
              value={testTo}
              onChange={e => setTestTo(e.target.value)}
              placeholder="recipient@example.com"
              className={cn(inputClassName, 'max-w-xs')}
            />
            <Button
              variant="secondary"
              loading={sendTestEmail.isPending}
              onClick={() => { setTestMsg(null); setTestError(null); sendTestEmail.mutate() }}
              disabled={!testTo}
            >
              Send
            </Button>
          </div>
          {testMsg && <p className="mt-1 text-xs text-[var(--color-success)]">{testMsg}</p>}
          {testError && <p className="mt-1 text-xs text-[var(--color-danger)]">{testError}</p>}
        </SettingsField>
      </SettingsSection>
    </div>
  )
}
