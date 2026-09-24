import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection, SettingsSelect } from '../../components/settings/settings-ui'
import { Switch } from '../../components/ui/switch'
import type { GeneralSettings } from '../../types'

const DEFAULT: GeneralSettings = {
  default_resume_market: '',
  require_review_before_submit: true,
  job_suitability_score: 7,
  max_jobs_per_keyword: 25,
  halal_job_filter: false,
}

export function ApplicationSettingsPage() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })
  const { data: markets = [] } = useQuery({ queryKey: ['markets'], queryFn: settingsApi.markets.list })
  const [form, setForm] = useState<GeneralSettings>(DEFAULT)
  const [saved, setSaved] = useState(false)

  useEffect(() => { if (data) setForm(data) }, [data])

  const save = useMutation({
    mutationFn: () => settingsApi.general.set(form),
    onSuccess: () => {
      qc.setQueryData(['settings-general'], form)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  function set<K extends keyof GeneralSettings>(key: K, value: GeneralSettings[K]) {
    setForm(f => ({ ...f, [key]: value }))
  }

  const threshold = form.job_suitability_score ?? 7

  return (
    <div className="space-y-6">
      <PageHeader
        title="Application"
        description="Control how Jobifai searches, scores, and submits applications."
      />

      <SettingsSection title="Application behaviour" description="Changes apply to the next automation run.">
        <Switch
          label="Review before submission"
          helper="Require your approval before Jobifai submits an application."
          checked={form.require_review_before_submit ?? false}
          onCheckedChange={v => set('require_review_before_submit', v)}
        />
        <SettingsField
          label="Suitability threshold"
          sub={`Skip roles scoring below ${threshold}/10`}
          layout="column"
        >
          <div className="flex items-center gap-4">
            <input
              type="range"
              min={0}
              max={10}
              step={1}
              value={threshold}
              onChange={e => set('job_suitability_score', Number(e.target.value))}
              className="flex-1 max-w-xs accent-[var(--color-accent)]"
              aria-label="Suitability threshold"
            />
            <span className="text-sm font-semibold tabular-nums text-[var(--color-text)] w-6">{threshold}</span>
          </div>
        </SettingsField>
        <SettingsField label="Maximum jobs per keyword" sub="How many listings to collect per search before processing">
          <SettingsNumberInput
            value={form.max_jobs_per_keyword ?? 25}
            onChange={v => set('max_jobs_per_keyword', v)}
            min={1}
            max={200}
          />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Employment ethics">
        <Switch
          label="Halal job filter"
          helper="Flags positions whose employer or responsibilities may need additional review."
          checked={form.halal_job_filter ?? false}
          onCheckedChange={v => set('halal_job_filter', v)}
        />
      </SettingsSection>

      <SettingsSection title="Resume generation">
        <SettingsField label="Default market" sub="Adjusts prompts and resume styling for your target region">
          <SettingsSelect
            value={form.default_resume_market ?? ''}
            onChange={e => set('default_resume_market', e.target.value)}
          >
            <option value="">None</option>
            {markets.map(m => <option key={m.name} value={m.name}>{m.name}</option>)}
          </SettingsSelect>
        </SettingsField>
        <Switch
          label="Generate tailored documents"
          helper={
            form.generate_new_resume_docs
              ? 'A tailored resume (and cover letter when applicable) is generated for each application.'
              : 'Use your existing on-platform resume without generating new documents.'
          }
          checked={form.generate_new_resume_docs ?? false}
          onCheckedChange={v => set('generate_new_resume_docs', v)}
        />
      </SettingsSection>

      <Button
        variant="primary"
        fullWidth
        loading={save.isPending}
        leftIcon={saved ? <Check size={14} /> : undefined}
        onClick={() => save.mutate()}
      >
        {saved ? 'Saved' : 'Save changes'}
      </Button>

      {save.isError && (
        <p className="text-xs text-[var(--color-danger)] text-center">{(save.error as Error).message}</p>
      )}
    </div>
  )
}
