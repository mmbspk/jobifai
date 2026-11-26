import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { jobPagesApi } from '../../api/job-pages'
import type { ReadingConfig, ReadingMode } from '../../api/job-pages'
import { Button } from '../Button'
import { SettingsField, SettingsNumberInput, SettingsSection } from '../settings/settings-ui'
import { inputClassName } from '../ui/input'

export function JobPageSettings() {
  const qc = useQueryClient()
  const settings = useQuery({ queryKey: ['admin-job-pages'], queryFn: jobPagesApi.get })
  const recipes = useQuery({ queryKey: ['admin-job-page-recipes'], queryFn: jobPagesApi.recipes })
  const events = useQuery({ queryKey: ['admin-job-page-events'], queryFn: jobPagesApi.events })
  const [draft, setDraft] = useState<ReadingConfig | null>(null)
  const [pilotText, setPilotText] = useState<string | null>(null)
  const [url, setUrl] = useState('')
  const config = draft ?? settings.data
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['admin-job-pages'] })
    void qc.invalidateQueries({ queryKey: ['admin-job-page-recipes'] })
    void qc.invalidateQueries({ queryKey: ['admin-job-page-events'] })
  }
  const save = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('Reader settings unavailable')
      return jobPagesApi.set({ ...config, pilot_users: (pilotText ?? config.pilot_users?.join('\n') ?? '').split(/[\s,]+/).filter(Boolean) })
    },
    onSuccess: data => { qc.setQueryData(['admin-job-pages'], data); setDraft(null); setPilotText(null); refresh() },
  })
  const retire = useMutation({ mutationFn: jobPagesApi.retire, onSuccess: refresh })
  const canary = useMutation({ mutationFn: () => jobPagesApi.canary(url.trim()), onSuccess: refresh })
  const update = (patch: Partial<ReadingConfig>) => { if (config) setDraft({ ...config, ...patch }) }
  return <SettingsSection title="Job description reading">
    <p className="text-sm text-muted-foreground">Recover changed LinkedIn and SEEK pages and reuse verified reading methods. Known pages continue using the standard reader. Changes take effect during active reads.</p>
    {settings.isError ? <p role="alert">Could not load reader settings. Reload this page to try again.</p> : null}
    {config ? <>
      <div className="grid gap-4 sm:grid-cols-2">
        {(['recovery', 'reuse'] as const).map(key => <SettingsField key={key} label={key === 'recovery' ? 'Page recovery' : 'Verified recipe reuse'}>
          <select aria-label={key === 'recovery' ? 'Page recovery' : 'Verified recipe reuse'} className={inputClassName} value={config[key]} onChange={e => update({ [key]: e.target.value as ReadingMode })}>
            <option value="off">Off</option><option value="pilot">Pilot accounts</option><option value="on">All accounts</option>
          </select>
        </SettingsField>)}
      </div>
      {config.recovery === 'pilot' || config.reuse === 'pilot' ? <SettingsField label="Reader pilot account IDs" sub="One account ID per line. Accounts outside the pilot use the standard reader.">
        <textarea aria-label="Reader pilot account IDs" rows={3} className={inputClassName} value={pilotText ?? config.pilot_users?.join('\n') ?? ''} onChange={e => setPilotText(e.target.value)} />
      </SettingsField> : null}
      <details><summary className="cursor-pointer text-sm font-medium">Reading limits</summary>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          {([{ key: 'max_requests', label: 'Maximum AI requests', max: 8 }, { key: 'max_tokens', label: 'Maximum AI tokens', max: 100000 }, { key: 'wall_clock_seconds', label: 'Reading deadline (seconds)', max: 120 }, { key: 'max_recovery_actions', label: 'Maximum recovery actions', max: 8 }] as const).map(field => <SettingsField key={field.key} label={field.label}>
            <SettingsNumberInput value={config.limits[field.key]} min={1} max={field.max} onChange={v => update({ limits: { ...config.limits, [field.key]: v } })} />
          </SettingsField>)}
          <SettingsField label="Maximum AI cost (USD)"><input aria-label="Maximum AI cost (USD)" className={inputClassName} type="number" value={config.limits.max_cost_micro_usd / 1000000} min={0.001} max={2} step={0.01} onChange={e => update({ limits: { ...config.limits, max_cost_micro_usd: Math.round(Number(e.target.value) * 1000000) } })} /></SettingsField>
        </div>
      </details>
      {save.isError ? <p role="alert">Could not save reader settings. Reload if another admin changed them.</p> : null}
      {save.isSuccess ? <p role="status">Reader settings saved.</p> : null}
      <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>Save reader settings</Button>
    </> : null}
    <details>
      <summary className="cursor-pointer text-sm font-medium">Verified methods and recent reads</summary>
      <div className="mt-4 space-y-4">
        {recipes.isError ? <p role="alert">Could not load verified methods.</p> : recipes.data?.recipes.length ? recipes.data.recipes.slice(0, 20).map(recipe => <div key={recipe.id} className="flex flex-wrap items-center justify-between gap-3 border-b pb-3 text-sm">
          <div><p className="font-medium">{recipe.scope.platform === 'linkedin' ? 'LinkedIn' : 'SEEK'}: {recipe.state}</p><p className="text-muted-foreground">Verified {new Date(recipe.verified_at).toLocaleString()} · version {recipe.version}</p><details><summary>Method evidence</summary><p className="break-all">{recipe.id}<br />{recipe.verification_kind}<br />{recipe.layout_signature}</p></details></div>
          {recipe.state !== 'retired' ? <Button variant="secondary" disabled={retire.isPending} onClick={() => retire.mutate(recipe.id)}>Retire method</Button> : null}
        </div>) : <p className="text-sm text-muted-foreground">No verified methods yet. Successful recovery adds reusable methods here.</p>}
        {retire.isError ? <p role="alert">Could not retire this method.</p> : null}
        {events.isError ? <p role="alert">Could not load recent reads.</p> : events.data?.events.slice(0, 8).map(event => <p key={event.id} className="text-sm">{event.scope.platform} — {event.failure_code || event.source} · {event.metrics.http_attempts} AI requests · {(event.metrics.latency_ms / 1000).toFixed(1)}s · {event.metrics.cost_known ? '$' + (event.metrics.cost_micro_usd / 1000000).toFixed(4) : 'Cost unavailable'}</p>)}
      </div>
    </details>
    <details>
      <summary className="cursor-pointer text-sm font-medium">Test a listing in the worker</summary>
      <div className="mt-4 space-y-3">
        <p className="text-sm text-muted-foreground">Uses your saved jobsite session and the current reader settings. Browser and AI usage follow the usual limits. This reads the listing without submitting an application.</p>
        <SettingsField label="Listing URL"><input aria-label="Reader test listing URL" type="url" className={inputClassName} value={url} onChange={e => { setUrl(e.target.value); canary.reset() }} placeholder="https://www.linkedin.com/jobs/view/…" /></SettingsField>
        <Button variant="secondary" disabled={!url.trim()} loading={canary.isPending} onClick={() => canary.mutate()}>Read listing</Button>
        {canary.isError ? <p role="alert">Could not run the worker test. Check your saved session and try again.</p> : null}
        {canary.data ? <div role="status" className="space-y-2 text-sm">
          <p>{canary.data.success ? 'Verified ' + canary.data.description_characters + ' description characters via ' + canary.data.result.source + '.' : canary.data.message}</p>
          {canary.data.success ? <><p className="font-medium">{canary.data.result.identity.title} · {canary.data.result.identity.company}</p><details><summary>Read description</summary><p className="mt-2 whitespace-pre-wrap">{canary.data.result.description}</p></details></> : <p>{canary.data.failure_code}</p>}
        </div> : null}
      </div>
    </details>
  </SettingsSection>
}
