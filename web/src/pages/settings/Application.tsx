import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { Button } from '../../components/Button'
import { cn } from '../../lib'
import type { GeneralSettings } from '../../types'

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
      <div className="py-3 pr-4 pl-3 border-b border-[var(--color-border)] border-l-2 border-l-[var(--color-accent)] text-xs font-semibold text-[var(--color-text-muted)] uppercase tracking-wider">
        {title}
      </div>
      <div className="p-4 space-y-4">{children}</div>
    </div>
  )
}

function Field({ label, sub, children }: { label: string; sub?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <div className="text-sm text-[var(--color-text)]">{label}</div>
        {sub && <div className="text-xs text-[var(--color-text-dim)]">{sub}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative w-10 h-5 rounded-full transition-colors',
        checked ? 'bg-[var(--color-accent)]' : 'bg-[var(--color-border)]',
      )}
    >
      <span
        className={cn(
          'absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow transition-transform',
          checked && 'translate-x-5',
        )}
      />
    </button>
  )
}

function NumInput({ value, onChange, min, max }: { value: number; onChange: (v: number) => void; min?: number; max?: number }) {
  return (
    <input
      type="number"
      value={value}
      min={min}
      max={max}
      onChange={e => onChange(Number(e.target.value))}
      className="w-24 bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)] focus:ring-1 focus:ring-[var(--color-accent)]/20 transition-colors tabular-nums"
    />
  )
}

const DEFAULT: GeneralSettings = {
  default_resume_market: '',
  require_review_before_submit: true,
  job_suitability_score: 7,
  max_jobs_per_keyword: 25,
  halal_job_filter: false,
  interview_questions_enabled: true,
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

  return (
    <div className="space-y-4">
      <Section title="Resume Defaults">
        <Field label="Market" sub="Target job market, adjusts prompts and resume styling">
          <select value={form.default_resume_market ?? ''} onChange={e => set('default_resume_market', e.target.value)}
            className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)] focus:ring-1 focus:ring-[var(--color-accent)]/20 transition-colors">
            <option value="">None</option>
            {markets.map(m => <option key={m.name} value={m.name}>{m.name}</option>)}
          </select>
        </Field>
        <Field
          label="Generate New Resume / Cover Letter"
          sub={
            form.generate_new_resume_docs
              ? 'A tailored resume (and cover letter when applicable) will be generated for each application using your profile and the job description.'
              : 'Your existing resume on the job site will be used. No new documents are generated, saves tokens for large batches.'
          }
        >
          <Toggle checked={form.generate_new_resume_docs ?? false} onChange={v => set('generate_new_resume_docs', v)} />
        </Field>
      </Section>

      <Section title="Job Filtering">
        <Field label="Suitability Threshold" sub={`Skip jobs scoring below ${form.job_suitability_score ?? 7}/10`}>
          <div className="flex items-center gap-3">
            <input type="range" min={0} max={10} step={1} value={form.job_suitability_score ?? 7}
              onChange={e => set('job_suitability_score', Number(e.target.value))}
              className="w-28 accent-violet-500"
            />
            <span className="text-sm font-mono text-[var(--color-text)] w-4">{form.job_suitability_score ?? 7}</span>
          </div>
        </Field>
        <Field label="Max Jobs Per Keyword" sub="How many jobs to collect per search keyword before processing (default 25)">
          <NumInput value={form.max_jobs_per_keyword ?? 25} onChange={v => set('max_jobs_per_keyword', v)} min={1} max={200} />
        </Field>
        <Field label="Require Review Before Submit" sub="Pause for manual approval before each application">
          <Toggle checked={form.require_review_before_submit ?? false} onChange={v => set('require_review_before_submit', v)} />
        </Field>
        <Field label="Halal Job Filter" sub="Automatically skip jobs that are impermissible or doubtful under Islamic employment ethics">
          <Toggle checked={form.halal_job_filter ?? false} onChange={v => set('halal_job_filter', v)} />
        </Field>
        <Field label="Interview Questions" sub="Show the Questions tab in Generate to answer application or interview questions using your profile">
          <Toggle checked={form.interview_questions_enabled ?? true} onChange={v => set('interview_questions_enabled', v)} />
        </Field>
      </Section>

      <Button
        variant="primary"
        fullWidth
        loading={save.isPending}
        leftIcon={saved ? <Check size={14} /> : undefined}
        onClick={() => save.mutate()}
      >
        {saved ? 'Saved' : 'Save Application Settings'}
      </Button>

      {save.isError && (
        <div className="text-xs text-red-400 text-center">{(save.error as Error).message}</div>
      )}
    </div>
  )
}
