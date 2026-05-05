import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { Eye, EyeOff, Check, MonitorCheck, Trash2 } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { authApi } from '../../api/auth'
import { getToken, apiGet } from '../../api/client'
import { PlatformBadge } from '../../components/PlatformBadge'

interface MaskedInputProps {
  readonly value: string
  readonly onSave: (v: string) => Promise<void>
  readonly onDelete?: () => Promise<void>
  readonly label: string
}

function MaskedInput({ value, onSave, onDelete, label }: MaskedInputProps) {
  const [editing, setEditing] = useState(false)
  const [val, setVal] = useState('')
  const [show, setShow] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)

  async function save() {
    setSaving(true)
    await onSave(val)
    setSaving(false)
    setEditing(false)
    setVal('')
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter') { save() }
    else if (e.key === 'Escape') { setEditing(false) }
  }

  async function handleDelete() {
    if (!onDelete) return
    setDeleting(true)
    await onDelete()
    setDeleting(false)
  }

  if (!editing) {
    return (
      <div className="flex items-center gap-3">
        <span className="text-sm font-mono text-[var(--color-text-muted)]">
          {value ? '•'.repeat(16) : <span className="text-[var(--color-text-dim)] not-italic font-sans">Not set</span>}
        </span>
        <button onClick={() => setEditing(true)} className="text-xs text-violet-400 hover:text-violet-300">
          {value ? 'Update' : 'Set'}
        </button>
        {value && onDelete && (
          <button onClick={handleDelete} disabled={deleting} className="text-xs text-[var(--color-danger)] hover:opacity-80 disabled:opacity-50">
            {deleting ? '…' : 'Delete'}
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="flex items-center gap-2">
      <div className="relative flex-1">
        <input
          autoFocus
          type={show ? 'text' : 'password'}
          value={val}
          onChange={e => setVal(e.target.value)}
          placeholder={`Enter ${label}`}
          className="w-full bg-[var(--color-surface-2)] border border-violet-500/50 rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] outline-none pr-8"
          onKeyDown={handleKeyDown}
        />
        <button onClick={() => setShow(v => !v)} className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--color-text-dim)]">
          {show ? <EyeOff size={13} /> : <Eye size={13} />}
        </button>
      </div>
      <button onClick={save} disabled={!val || saving} className="px-3 py-1.5 bg-violet-500 text-white rounded-lg text-xs disabled:opacity-50">
        {saving ? '…' : 'Save'}
      </button>
      <button onClick={() => setEditing(false)} className="text-xs text-[var(--color-text-dim)]">Cancel</button>
    </div>
  )
}

const PLATFORMS = ['linkedin', 'seek'] as const

interface CredentialsCardProps {
  readonly platform: string
  readonly hasCreds: boolean
}

// Credentials form split out to reduce cognitive complexity of the parent card
function CredsForm({ platform, hasCreds }: { readonly platform: string; readonly hasCreds: boolean }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  async function save() {
    setSaving(true)
    await settingsApi.secrets.setCredentials(platform, email, password)
    qc.invalidateQueries({ queryKey: ['settings-secrets'] })
    setSaving(false)
    setSaved(true)
    setOpen(false)
    setTimeout(() => setSaved(false), 2000)
  }

  let buttonLabel = 'Add credentials'
  if (open) buttonLabel = 'Cancel'
  else if (hasCreds) buttonLabel = 'Credentials'

  return (
    <>
      <div className="flex items-center gap-1.5">
        {hasCreds && <span className="text-xs text-[var(--color-text-dim)]">· credentials saved</span>}
        {saved && <Check size={13} className="text-emerald-400" />}
        <button onClick={() => setOpen(v => !v)} className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)]">
          {buttonLabel}
        </button>
      </div>
      {open && (
        <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)]">
          <div className="pt-3 space-y-2">
            <input value={email} onChange={e => setEmail(e.target.value)} type="email" placeholder="Email"
              className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50"
            />
            <div className="relative">
              <input value={password} onChange={e => setPassword(e.target.value)} type={showPw ? 'text' : 'password'} placeholder="Password"
                className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50 pr-8"
              />
              <button onClick={() => setShowPw(v => !v)} className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--color-text-dim)]">
                {showPw ? <EyeOff size={13} /> : <Eye size={13} />}
              </button>
            </div>
          </div>
          <button onClick={save} disabled={!email || !password || saving}
            className="w-full py-1.5 bg-violet-500 text-white rounded-lg text-sm disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save credentials'}
          </button>
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
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-3">
          <PlatformBadge platform={platform} />
          {hasSession
            ? <span className="text-xs text-emerald-400 flex items-center gap-1"><MonitorCheck size={11} /> Session active</span>
            : <span className="text-xs text-[var(--color-text-dim)]">No session</span>
          }
        </div>
        <div className="flex items-center gap-3">
          {hasSession
            ? <button onClick={() => disconnect.mutate()} className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-danger)] flex items-center gap-1 transition-colors">
                <Trash2 size={11} /> Disconnect
              </button>
            : <button onClick={() => { setSessionError(null); launch.mutate() }} disabled={launch.isPending || !!sessionId}
                className="text-xs text-violet-400 hover:text-violet-300 disabled:opacity-50"
              >
                {launchLabel}
              </button>
          }
          <CredsForm platform={platform} hasCreds={hasCreds} />
        </div>
      </div>

      {sessionId && (
        <div className="px-4 pb-4 border-t border-[var(--color-border-subtle)] pt-3 space-y-3">
          <div className="flex items-center justify-between">
            <p className="text-xs text-[var(--color-text-muted)]">
              Log in to <span className="capitalize">{platform}</span> below, then click <strong>Save session</strong>.
            </p>
            <button onClick={() => saveSession.mutate()} disabled={saveSession.isPending}
              className="px-4 py-1.5 bg-emerald-600 hover:bg-emerald-500 text-white rounded-lg text-xs font-medium disabled:opacity-50 transition-colors flex-shrink-0 ml-3"
            >
              {saveSession.isPending ? 'Saving…' : 'Save session'}
            </button>
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

export function Secrets() {
  const qc = useQueryClient()
  const { data: secrets } = useQuery({ queryKey: ['settings-secrets'], queryFn: settingsApi.secrets.get })

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--color-border)] text-xs font-medium text-[var(--color-text-muted)] uppercase tracking-wider">LLM API Keys</div>
        <div className="p-4 space-y-4">
          <div>
            <div className="text-sm text-[var(--color-text)] mb-2">API Key</div>
            <MaskedInput
              label="API key"
              value={secrets?.llm_api_key ?? ''}
              onSave={v => settingsApi.secrets.setApiKey('llm_api_key', v).then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
              onDelete={() => settingsApi.secrets.deleteApiKey().then(() => qc.invalidateQueries({ queryKey: ['settings-secrets'] }))}
            />
          </div>
        </div>
      </div>

      <div>
        <div className="text-xs font-medium text-[var(--color-text-muted)] uppercase tracking-wider mb-3">Platform Connections</div>
        <div className="space-y-2">
          {PLATFORMS.map(p => (
            <CredentialsCard
              key={p}
              platform={p}
              hasCreds={secrets?.credential_platforms?.includes(p) ?? false}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
