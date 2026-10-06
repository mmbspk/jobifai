import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection, SettingsSelect } from '../../components/settings/settings-ui'
import { Switch } from '../../components/ui/switch'
import type { CoverDocumentMode, DocumentPolicies, GeneralSettings, ResumeDocumentMode } from '../../types'
import { Link } from 'react-router-dom'

const DEFAULT: GeneralSettings = {
  default_resume_market: '',
  require_review_before_submit: true,
  job_suitability_score: 7,
  max_jobs_per_keyword: 25,
  halal_job_filter: false,
  human_behavior: { daily_application_limit: 5 },
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

  function setDocPolicy(patch: Partial<DocumentPolicies>) {
    setForm(f => ({
      ...f,
      document_policies: { ...f.document_policies, ...patch, version: 1 },
      generate_new_resume_docs: patch.resume_mode
        ? patch.resume_mode === 'tailor'
        : f.document_policies?.resume_mode === 'tailor' || f.generate_new_resume_docs,
    }))
  }

  const threshold = form.job_suitability_score ?? 7

  return (
    <div className="space-y-6">
      <PageHeader
        title="Application"
        description="Control how Jobifai searches, scores, and submits applications."
      />

      <SettingsSection title="Application behaviour" description="Changes apply to the next automation run.">
        <div id="daily-applications">
          <SettingsField
            label="Daily applications"
            sub="Maximum applications Jobifai may submit for you per calendar day"
          >
            <SettingsNumberInput
              value={form.human_behavior?.daily_application_limit ?? 5}
              onChange={v =>
                setForm(f => ({
                  ...f,
                  human_behavior: { ...f.human_behavior, daily_application_limit: v },
                }))
              }
              min={1}
              max={200}
            />
          </SettingsField>
        </div>
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

      <SettingsSection
        title="Resume and documents"
        description="Manage defaults in Documents. PDF rendering and reusing saved versions do not use AI credits."
      >
        <p className="text-sm text-[var(--color-text-dim)]">
          <Link to="/documents" className="text-[var(--color-accent)] underline">Documents</Link>
          {' '}is where you create defaults, upload originals, and preview PDFs.
        </p>
        <SettingsField label="Default market" sub="Regional prompts and styling for generated or render-only PDFs">
          <SettingsSelect
            value={form.default_resume_market ?? ''}
            onChange={e => set('default_resume_market', e.target.value)}
          >
            <option value="">None</option>
            {markets.map(m => <option key={m.name} value={m.name}>{m.name}</option>)}
          </SettingsSelect>
        </SettingsField>
        <SettingsField label="Resume policy" sub="How Jobifai chooses a resume for each application">
          <SettingsSelect
            value={form.document_policies?.resume_mode ?? 'default'}
            onChange={e => setDocPolicy({ resume_mode: e.target.value as ResumeDocumentMode })}
          >
            <option value="default">Use my selected default</option>
            <option value="tailor">Tailor for each job (uses AI credits)</option>
            <option value="site">Use the job-site resume</option>
          </SettingsSelect>
        </SettingsField>
        <SettingsField label="Cover letter policy" sub="Cover generation uses AI credits; optional skips respect your choice">
          <SettingsSelect
            value={form.document_policies?.cover_mode ?? 'when_required'}
            onChange={e => setDocPolicy({ cover_mode: e.target.value as CoverDocumentMode })}
          >
            <option value="when_required">Generate when required</option>
            <option value="when_accepted">Generate whenever the form accepts one</option>
            <option value="general_default">Use my general default</option>
            <option value="skip_optional">Skip optional cover letters</option>
          </SettingsSelect>
        </SettingsField>
        <SettingsField label="Cover letter tone" sub="Writing style for AI-generated cover letters — adjust word choice and formality">
          <SettingsSelect
            value={form.cover_letter_tone ?? ''}
            onChange={e => set('cover_letter_tone', e.target.value)}
          >
            <option value="">Default (direct, first person)</option>
            <option value="formal">Formal</option>
            <option value="conversational">Conversational</option>
            <option value="confident">Confident</option>
          </SettingsSelect>
        </SettingsField>
        <Switch
          label="Allow job-site resume if default is missing"
          helper="Only when you explicitly enable this fallback. Otherwise applications hold for review."
          checked={form.document_policies?.fallback?.allow_site_resume_when_default_missing ?? false}
          onCheckedChange={v =>
            setDocPolicy({
              fallback: {
                ...form.document_policies?.fallback,
                allow_site_resume_when_default_missing: v,
              },
            })
          }
        />
        <Switch
          label="Allow general cover default if generation fails"
          helper="Optional cover failures otherwise follow your policy and may hold required applications."
          checked={form.document_policies?.fallback?.allow_general_cover_when_generate_fails ?? false}
          onCheckedChange={v =>
            setDocPolicy({
              fallback: {
                ...form.document_policies?.fallback,
                allow_general_cover_when_generate_fails: v,
              },
            })
          }
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
