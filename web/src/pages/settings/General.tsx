import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { useAuth } from '../../contexts/AuthContext'
import { Button } from '../../components/Button'
import { cn } from '../../lib'
import type { GeneralSettings, TaskModel } from '../../types'

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

function TextInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <input
      value={value}
      onChange={e => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-48 bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)] focus:ring-1 focus:ring-[var(--color-accent)]/20 transition-colors"
    />
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
  llm: { provider: 'claude', model: 'claude-sonnet-4-6', use_proxy: false, proxy_url: '', task_models: {} },
  browser: { show_browser: true, use_chrome_profile: true, chrome_profile_path: '', remote_debug_port: 0 },
  human_behavior: {
    daily_application_limit: 40, job_read_time_min: 10, job_read_time_max: 30,
    pause_between_jobs_min: 5, pause_between_jobs_max: 15,
    interaction_pause_min: 0.4, interaction_pause_max: 1.8,
    typing_speed_min: 0.04, typing_speed_max: 0.12,
    business_hours_start: 0, business_hours_end: 0,
  },
  default_resume_market: '',
  require_review_before_submit: true,
  job_suitability_score: 7,
  max_jobs_per_keyword: 25,
  halal_job_filter: false,
  interview_questions_enabled: true,
}

export function GeneralSettingsPage() {
  const qc = useQueryClient()
  const { user } = useAuth()
  const isAdmin = user?.is_admin === true
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

  function llm<K extends keyof NonNullable<GeneralSettings['llm']>>(key: K, value: NonNullable<GeneralSettings['llm']>[K]) {
    setForm(f => ({ ...f, llm: { ...f.llm, [key]: value } }))
  }

  function setTaskModel(task: string, key: keyof TaskModel, value: unknown) {
    setForm(f => ({
      ...f,
      llm: {
        ...f.llm,
        task_models: {
          ...f.llm?.task_models,
          [task]: { ...f.llm?.task_models?.[task], [key]: value },
        },
      },
    }))
  }

  function browser<K extends keyof NonNullable<GeneralSettings['browser']>>(key: K, value: NonNullable<GeneralSettings['browser']>[K]) {
    setForm(f => ({ ...f, browser: { ...f.browser, [key]: value } }))
  }

  function hb<K extends keyof NonNullable<GeneralSettings['human_behavior']>>(key: K, value: NonNullable<GeneralSettings['human_behavior']>[K]) {
    setForm(f => ({ ...f, human_behavior: { ...f.human_behavior, [key]: value } }))
  }

  const llmCfg = form.llm ?? {}
  const browserCfg = form.browser ?? {}
  const hbCfg = form.human_behavior ?? {}

  return (
    <div className="space-y-4">
      <Section title="LLM Configuration">
        <Field label="Provider">
          <select value={llmCfg.provider ?? ''} onChange={e => llm('provider', e.target.value)}
            className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)] focus:ring-1 focus:ring-[var(--color-accent)]/20 transition-colors">
            <option value="claude">Claude (Anthropic)</option>
            <option value="openai">OpenAI</option>
            <option value="ollama">Ollama (local)</option>
          </select>
        </Field>
        <Field label="Model">
          <TextInput value={llmCfg.model ?? ''} onChange={v => llm('model', v)} placeholder="claude-sonnet-4-6" />
        </Field>
        {isAdmin && (
          <Field label="Use Proxy" sub="Route LLM calls through a proxy server">
            <Toggle checked={llmCfg.use_proxy ?? false} onChange={v => llm('use_proxy', v)} />
          </Field>
        )}
        {isAdmin && llmCfg.use_proxy && (
          <Field label="Proxy URL">
            <TextInput value={llmCfg.proxy_url ?? ''} onChange={v => llm('proxy_url', v)} placeholder="http://localhost:6655/anthropic" />
          </Field>
        )}
        <Field label="Max Tokens" sub="Maximum tokens per LLM response (0 = model default)">
          <TextInput
            value={llmCfg.max_tokens != null && llmCfg.max_tokens > 0 ? String(llmCfg.max_tokens) : ''}
            onChange={v => llm('max_tokens', v === '' ? 0 : Number.parseInt(v, 10) || 0)}
            placeholder="0 (model default)"
          />
        </Field>
      </Section>

      <Section title="Per-Task Model Overrides">
        {[
          { key: 'scoring',      label: 'Suitability Scoring',  hint: 'Once per job. Haiku recommended.' },
          { key: 'halal',        label: 'Halal Filter',          hint: 'No profile sent. Haiku recommended.' },
          { key: 'tailoring',    label: 'Resume Tailoring',      hint: 'Rewrites full profile. Sonnet+ recommended.' },
          { key: 'cover_letter', label: 'Cover Letter',          hint: 'Prose writing. Haiku or Sonnet.' },
          { key: 'form_filling', label: 'Form Q&A',              hint: 'Per question. Haiku recommended.' },
          { key: 'questions',    label: 'Interview Questions',   hint: 'Batch question answering. Haiku or Sonnet.' },
        ].map(t => (
          <Field key={t.key} label={t.label} sub={t.hint}>
            <TextInput
              value={llmCfg.task_models?.[t.key]?.model ?? ''}
              onChange={v => setTaskModel(t.key, 'model', v || undefined)}
              placeholder={llmCfg.model ?? 'default model'}
            />
          </Field>
        ))}
      </Section>

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

      {isAdmin && (
      <Section title="Browser">
        <Field label="Show Browser Window">
          <Toggle checked={browserCfg.show_browser ?? true} onChange={v => browser('show_browser', v)} />
        </Field>
        <Field label="Use Chrome Profile" sub="Reuse a persistent Chrome profile (keeps Auth0/localStorage login). Leave path empty to use data/chrome-profiles/&lt;user&gt;/&lt;platform&gt;">
          <Toggle checked={browserCfg.use_chrome_profile ?? true} onChange={v => browser('use_chrome_profile', v)} />
        </Field>
        {browserCfg.use_chrome_profile && (
          <Field label="Profile Path">
            <TextInput value={browserCfg.chrome_profile_path ?? ''} onChange={v => browser('chrome_profile_path', v)} placeholder="data/chrome-profiles/... (default if empty)" />
          </Field>
        )}
        <Field label="Remote Debug Port" sub="Chrome DevTools remote debugging port (0 = disabled)">
          <NumInput value={browserCfg.remote_debug_port ?? 0} onChange={v => browser('remote_debug_port', v)} min={0} max={65535} />
        </Field>
      </Section>
      )}

      <Section title="Human Behaviour">
        <Field label="Daily Application Limit">
          <NumInput value={hbCfg.daily_application_limit ?? 40} onChange={v => hb('daily_application_limit', v)} min={1} max={200} />
        </Field>
        <Field label="Job Read Time (s)" sub="Min / Max seconds to read a job before applying">
          <div className="flex items-center gap-2">
            <NumInput value={hbCfg.job_read_time_min ?? 10} onChange={v => hb('job_read_time_min', v)} min={1} />
            <span className="text-[var(--color-text-dim)] text-xs">–</span>
            <NumInput value={hbCfg.job_read_time_max ?? 30} onChange={v => hb('job_read_time_max', v)} min={1} />
          </div>
        </Field>
        <Field label="Pause Between Jobs (s)">
          <div className="flex items-center gap-2">
            <NumInput value={hbCfg.pause_between_jobs_min ?? 5} onChange={v => hb('pause_between_jobs_min', v)} min={0} />
            <span className="text-[var(--color-text-dim)] text-xs">–</span>
            <NumInput value={hbCfg.pause_between_jobs_max ?? 15} onChange={v => hb('pause_between_jobs_max', v)} min={0} />
          </div>
        </Field>
        <Field label="Business Hours" sub="Restrict to these hours (0 = disabled)">
          <div className="flex items-center gap-2">
            <NumInput value={hbCfg.business_hours_start ?? 0} onChange={v => hb('business_hours_start', v)} min={0} max={23} />
            <span className="text-[var(--color-text-dim)] text-xs">–</span>
            <NumInput value={hbCfg.business_hours_end ?? 0} onChange={v => hb('business_hours_end', v)} min={0} max={23} />
          </div>
        </Field>
      </Section>

      <Button
        variant="primary"
        fullWidth
        loading={save.isPending}
        leftIcon={saved ? <Check size={14} /> : undefined}
        onClick={() => save.mutate()}
      >
        {saved ? 'Saved' : 'Save Settings'}
      </Button>

      {save.isError && (
        <div className="text-xs text-red-400 text-center">{(save.error as Error).message}</div>
      )}
    </div>
  )
}
