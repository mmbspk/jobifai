import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '../../api/admin'
import { Button } from '../Button'
import { SettingsField, SettingsSection } from '../settings/settings-ui'
import { Switch } from '../ui/switch'
import { inputClassName } from '../ui/input'
import type { ApplyEngineConfig, ApplyEngineMode } from '../../types'

const modes: ApplyEngineMode[] = ['legacy', 'shadow', 'adaptive']
const labels = { legacy: 'Legacy', shadow: 'Shadow', adaptive: 'Adaptive' }

export function ApplyEngineSettings() {
  const qc = useQueryClient()
  const settings = useQuery({ queryKey: ['admin-apply-engine'], queryFn: adminApi.applyEngine.get })
  const audit = useQuery({ queryKey: ['admin-apply-engine-audit'], queryFn: adminApi.applyEngine.audit })
  const [config, setConfig] = useState<ApplyEngineConfig>({ mode: 'legacy', external_ats_enabled: false })
  const [pilotIDs, setPilotIDs] = useState('')
  const [reason, setReason] = useState('')
  useEffect(() => {
    if (settings.data) {
      setConfig(settings.data)
      setPilotIDs((settings.data.pilot_user_ids ?? []).join('\n'))
    }
  }, [settings.data])
  const save = useMutation({
    mutationFn: () => adminApi.applyEngine.set({
      ...config,
      pilot_user_ids: [...new Set(pilotIDs.split(/[\s,]+/).filter(Boolean))],
    }, reason.trim() || undefined),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-apply-engine'] })
      void qc.invalidateQueries({ queryKey: ['admin-apply-engine-audit'] })
    },
  })
  const override = (platform: string, value: string) => {
    const next = { ...config.platform_overrides }
    if (value) next[platform] = value as ApplyEngineMode
    else delete next[platform]
    setConfig({ ...config, platform_overrides: next })
  }
  return (
    <SettingsSection title="AI apply engine">
      <p className="text-sm text-muted-foreground">Legacy uses the existing application flow. Shadow compares adaptive interpretations while Legacy operates the browser. Adaptive uses the new engine. Changes apply to new attempts; switching an active platform to Legacy stops adaptive actions at the next safe boundary.</p>
      {settings.isError ? <p role="alert">Could not load apply engine settings.</p> : null}
      <SettingsField label="Default engine">
        <select aria-label="Default engine" className={inputClassName} value={config.mode} onChange={e => setConfig({ ...config, mode: e.target.value as ApplyEngineMode })} disabled={!settings.data}>
          {modes.map(mode => <option key={mode} value={mode}>{labels[mode]}</option>)}
        </select>
      </SettingsField>
      {['linkedin', 'seek'].map(platform => (
        <SettingsField key={platform} label={platform === 'linkedin' ? 'LinkedIn engine' : 'SEEK engine'}>
          <select aria-label={platform + ' engine'} className={inputClassName} value={config.platform_overrides?.[platform] ?? ''} onChange={e => override(platform, e.target.value)} disabled={!settings.data}>
            <option value="">Use default</option>
            {modes.map(mode => <option key={mode} value={mode}>{labels[mode]}</option>)}
          </select>
        </SettingsField>
      ))}
      <SettingsField label="Pilot user IDs" sub="One ID per line. Leave empty to apply the selected modes to all users. Users outside the pilot continue using Legacy.">
        <textarea aria-label="Pilot user IDs" className={inputClassName} rows={3} value={pilotIDs} onChange={e => setPilotIDs(e.target.value)} disabled={!settings.data} />
      </SettingsField>
      <Switch label="External Greenhouse applications" helper="Allow verified employer handoffs during Adaptive attempts. Other hiring portals continue using manual application." checked={config.external_ats_enabled} onCheckedChange={value => setConfig({ ...config, external_ats_enabled: value })} />
      <SettingsField label="Reason for change" sub="Optional note recorded in the audit history.">
        <input aria-label="Reason for change" className={inputClassName} value={reason} onChange={e => setReason(e.target.value)} />
      </SettingsField>
      {save.isError ? <p role="alert">Could not save apply engine settings.</p> : null}
      {save.isSuccess ? <p role="status">Apply engine settings saved.</p> : null}
      <Button variant="primary" loading={save.isPending} disabled={!settings.data} onClick={() => save.mutate()}>Save apply engine</Button>
      {audit.data?.entries.length ? <div className="space-y-2 text-sm">
        <h3 className="font-medium">Recent engine changes</h3>
        {audit.data.entries.slice(0, 5).map(entry => <p key={entry.id}>{entry.old_mode || 'legacy'} → {entry.new_mode || 'legacy'} · {new Date(entry.created_at).toLocaleString()}{entry.reason ? ' · ' + entry.reason : ''}</p>)}
      </div> : null}
    </SettingsSection>
  )
}
