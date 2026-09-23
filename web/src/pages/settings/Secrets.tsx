import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { Check, Eye, EyeOff, MonitorCheck, Trash2 } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { authApi } from '../../api/auth'
import { getToken, apiGet } from '../../api/client'
import { PlatformBadge } from '../../components/PlatformBadge'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsSection } from '../../components/settings/settings-ui'
import { MaskedSecretField } from '../../components/settings/MaskedSecretField'
import { inputClassName } from '../../components/ui/input'
import { Button } from '../../components/ui/button'
import { cn } from '../../lib'

const PLATFORMS = ['linkedin', 'seek'] as const

interface CredentialsCardProps {
  readonly platform: string
  readonly hasCreds: boolean
}

function CredsForm({ platform, hasCreds }: { readonly platform: string; readonly hasCreds: boolean }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [deleting, setDeleting] = useState(false)

  async function save() {
    setSaving(true)
    await settingsApi.secrets.setCredentials(platform, email, password)
    qc.invalidateQueries({ queryKey: ['settings-secrets'] })
    setSaving(false)
    setSaved(true)
    setOpen(false)
    setTimeout(() => setSaved(false), 2000)
  }

  async function deleteCredentials() {
    setDeleting(true)
    await settingsApi.secrets.deleteCredentials(platform)
    qc.invalidateQueries({ queryKey: ['settings-secrets'] })
    setDeleting(false)
  }

  let buttonLabel = 'Add credentials'
  if (open) buttonLabel = 'Cancel'
  else if (hasCreds) buttonLabel = 'Update'

  return (
    <>
      <div className="flex items-center gap-1.5">
        {hasCreds && <span className="text-xs text-[var(--color-text-dim)]">· credentials saved</span>}
        {saved && <Check size={13} className="text-emerald-400" />}
        <button type="button" onClick={() => setOpen(v => !v)} className="text-xs font-medium text-[var(--color-accent)] hover:underline">
          {buttonLabel}
        </button>
        {hasCreds && !open && (
          <button type="button" onClick={deleteCredentials} disabled={deleting}
            className="text-xs text-[var(--color-danger)] hover:underline disabled:opacity-50 flex items-center gap-0.5">
            <Trash2 size={11} />{deleting ? '…' : 'Delete'}
          </button>
        )}
      </div>
      {open && (
        <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)]">
          <div className="pt-3 space-y-2">
            <input value={email} onChange={e => setEmail(e.target.value)} type="email" placeholder="Email"
              className={inputClassName}
            />
            <div className="relative">
              <input value={password} onChange={e => setPassword(e.target.value)} type={showPw ? 'text' : 'password'} placeholder="Password"
                className={cn(inputClassName, 'pr-10')}
              />
              <button type="button" onClick={() => setShowPw(v => !v)} className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--color-text-dim)] p-1">
                {showPw ? <EyeOff size={13} /> : <Eye size={13} />}
              </button>
            </div>
          </div>
          <Button variant="primary" fullWidth disabled={!email || !password || saving} loading={saving} onClick={() => void save()}>
            Save credentials
          </Button>
          <p className="text-xs text-[var(--color-text-dim)] text-center">Encrypted with AES-GCM using a machine-specific key</p>
        </div>
      )}
    </>
  )
}

function VNCFrame() {
  const token = getToken() ?? ''
  if (!token) return null
  const wsPath = encodeURIComponent(`novnc/websockify?token=${encodeURIComponent(token)}`)
  return (
    <iframe
      src={`/novnc/vnc.html?path=${wsPath}&autoconnect=1&resize=scale`}
      className="w-full rounded-lg border border-[var(--color-border)]"
      style={{ height: '500px' }}
      title="Remote browser"
      sandbox="allow-scripts allow-same-origin allow-forms"
    />
  )
}

function CredentialsCard({ platform, hasCreds }: CredentialsCardProps) {
  const qc = useQueryClient()
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [sessionError, setSessionError] = useState<string | null>(null)

  const { data: systemInfo } = useQuery({
    queryKey: ['system-info'],
    queryFn: () => apiGet<{ vnc_enabled: boolean }>('/system/info'),
    staleTime: Infinity,
  })
  const vncEnabled = systemInfo?.vnc_enabled === true

  const { data: session } = useQuery({
    queryKey: ['platform-session', platform],
    queryFn: () => authApi.platformStatus(platform),
  })

  const launch = useMutation({
    mutationFn: () => authApi.launchBrowser(platform),
    onSuccess: (res) => { setSessionId(res.session_id); setSessionError(null) },
    onError: (err: Error) => setSessionError(err.message),
  })

  const saveSession = useMutation({
    mutationFn: () => authApi.saveSession(sessionId!, platform),
    onSuccess: () => { setSessionId(null); qc.invalidateQueries({ queryKey: ['platform-session', platform] }) },
    onError: (err: Error) => setSessionError(err.message),
  })

  const disconnect = useMutation({
    mutationFn: () => authApi.deleteSession(platform),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['platform-session', platform] }),
    onError: (err: Error) => setSessionError(err.message),
  })

  const hasSession = session?.has_session ?? false

  let launchLabel = 'Connect browser'
  if (launch.isPending) launchLabel = 'Opening…'
  else if (sessionId) launchLabel = 'Browser open'

  return (
    <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-3">
        <div className="flex items-center gap-3">
          <PlatformBadge platform={platform} />
          {hasSession
            ? <span className="text-xs text-emerald-400 flex items-center gap-1"><MonitorCheck size={11} /> Session active</span>
            : <span className="text-xs text-[var(--color-text-dim)]">No session</span>
          }
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {hasSession
            ? <button type="button" onClick={() => disconnect.mutate()} className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-danger)] flex items-center gap-1 transition-colors">
                <Trash2 size={11} /> Disconnect
              </button>
            : <button type="button" onClick={() => { setSessionError(null); launch.mutate() }} disabled={launch.isPending || !!sessionId}
                className="text-xs font-medium text-[var(--color-accent)] hover:underline disabled:opacity-50"
              >
                {launchLabel}
              </button>
          }
          <CredsForm platform={platform} hasCreds={hasCreds} />
        </div>
      </div>

      {sessionId && (
        <div className="px-4 pb-4 border-t border-[var(--color-border-subtle)] pt-3 space-y-3">
          <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
            <p className="text-xs text-[var(--color-text-muted)]">
              Log in to <span className="capitalize">{platform}</span> fully (including MFA). For Seek, open a job and confirm you can see Quick Apply, then click <strong>Save session</strong>.
            </p>
            <Button variant="primary" size="sm" disabled={saveSession.isPending} loading={saveSession.isPending} onClick={() => saveSession.mutate()} className="shrink-0">
              Save session
            </Button>
          </div>
          {vncEnabled && <VNCFrame />}
        </div>
      )}

      {sessionError && (
        <div className="px-4 pb-3 text-xs text-[var(--color-danger)]">{sessionError}</div>
      )}
    </div>
  )
}

export function PlatformsSettingsPage() {
  const qc = useQueryClient()
  const { data: secrets } = useQuery({ queryKey: ['settings-secrets'], queryFn: settingsApi.secrets.get })

  return (
    <div className="space-y-6">
      <PageHeader
        title="Platforms"
        description="Connect job boards and configure the AI provider used for scoring and document generation."
      />

      <SettingsSection title="AI provider" description="Your API key is stored encrypted and never shown again after saving.">
        <SettingsField label="LLM API key" sub="Used when you run Generate, automation, and profile extraction">
          <MaskedSecretField
            configured={!!secrets?.llm_api_key}
            label="API key"
            onSave={v => settingsApi.secrets.setApiKey('llm_api_key', v).then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
            onDelete={() => settingsApi.secrets.deleteApiKey().then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
          />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Job board connections" description="Sign in so Jobifai can search listings and submit applications on your behalf.">
        <div className="space-y-2">
          {PLATFORMS.map(p => (
            <CredentialsCard
              key={p}
              platform={p}
              hasCreds={secrets?.credential_platforms?.includes(p) ?? false}
            />
          ))}
        </div>
      </SettingsSection>
    </div>
  )
}

export function AdminSecretsPage() {
  const qc = useQueryClient()
  const { data: secrets } = useQuery({ queryKey: ['settings-secrets'], queryFn: settingsApi.secrets.get })

  return (
    <div className="space-y-4">
      <SettingsSection title="LLM API keys">
        <SettingsField label="API key">
          <MaskedSecretField
            configured={!!secrets?.llm_api_key}
            label="API key"
            onSave={v => settingsApi.secrets.setApiKey('llm_api_key', v).then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
            onDelete={() => settingsApi.secrets.deleteApiKey().then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
          />
        </SettingsField>
      </SettingsSection>
    </div>
  )
}
