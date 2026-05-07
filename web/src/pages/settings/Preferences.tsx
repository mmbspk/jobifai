import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { TagInput } from '../../components/TagInput'
import { LocationTagInput } from '../../components/LocationTagInput'
import { Button } from '../../components/Button'
import type { WorkPreferences } from '../../types'

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
      <div className="py-3 pr-4 pl-3 border-b border-[var(--color-border)] border-l-2 border-l-[var(--color-accent)] text-xs font-semibold text-[var(--color-text-muted)] uppercase tracking-wider">{title}</div>
      <div className="p-4">{children}</div>
    </div>
  )
}

function Checkbox({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-2.5 cursor-pointer select-none">
      <div
        onClick={() => onChange(!checked)}
        className={`w-4 h-4 rounded border flex items-center justify-center transition-colors ${checked ? 'bg-[var(--color-accent)] border-[var(--color-accent)]' : 'border-[var(--color-border)]'}`}
      >
        {checked && <Check size={10} strokeWidth={3} className="text-white" />}
      </div>
      <span className="text-sm text-[var(--color-text-muted)]">{label}</span>
    </label>
  )
}

const DEFAULT: WorkPreferences = {
  remote: true, hybrid: true, onsite: false,
  experience_level: { entry: true, associate: true, mid_senior_level: true, senior: true },
  job_types: { full_time: true },
  date_filters: { week: true },
  positions: [], locations: [],
  company_blacklist: [], title_blacklist: [], location_blacklist: [],
  apply_once_at_company: true, max_applications: 50,
}

export function Preferences() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings-preferences'], queryFn: settingsApi.preferences.get })
  const [form, setForm] = useState<WorkPreferences>(DEFAULT)
  const [saved, setSaved] = useState(false)

  useEffect(() => { if (data) setForm(data) }, [data])

  const save = useMutation({
    mutationFn: () => settingsApi.preferences.set(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settings-preferences'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  function toggle<K extends 'experience_level' | 'job_types' | 'date_filters'>(section: K, key: string) {
    setForm(f => ({ ...f, [section]: { ...f[section], [key]: !((f[section] as Record<string, boolean>)[key]) } }))
  }

  return (
    <div className="space-y-4">
      <Section title="Work Type">
        <div className="flex gap-6">
          <Checkbox label="Remote" checked={form.remote ?? false} onChange={v => setForm(f => ({ ...f, remote: v }))} />
          <Checkbox label="Hybrid" checked={form.hybrid ?? false} onChange={v => setForm(f => ({ ...f, hybrid: v }))} />
          <Checkbox label="On-site" checked={form.onsite ?? false} onChange={v => setForm(f => ({ ...f, onsite: v }))} />
        </div>
      </Section>

      <Section title="Experience Level">
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          {(['internship','entry','associate','mid_senior_level','senior','director','executive'] as const).map(k => (
            <Checkbox key={k} label={k.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())}
              checked={(form.experience_level as Record<string, boolean>)?.[k] ?? false}
              onChange={() => toggle('experience_level', k)}
            />
          ))}
        </div>
      </Section>

      <Section title="Job Types">
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          {(['full_time','contract','part_time','temporary','internship','volunteer','other'] as const).map(k => (
            <Checkbox key={k} label={k.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())}
              checked={(form.job_types as Record<string, boolean>)?.[k] ?? false}
              onChange={() => toggle('job_types', k)}
            />
          ))}
        </div>
      </Section>

      <Section title="Date Filter">
        <div className="flex flex-wrap gap-6">
          {(['all_time','month','week','hours_24'] as const).map(k => (
            <Checkbox key={k} label={k.replace(/_/g, ' ')}
              checked={(form.date_filters as Record<string, boolean>)?.[k] ?? false}
              onChange={() => toggle('date_filters', k)}
            />
          ))}
        </div>
      </Section>

      <Section title="Search Targets">
        <div className="space-y-4">
          <div>
            <div className="text-xs text-[var(--color-text-dim)] mb-1.5">Job Titles / Positions</div>
            <div className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg p-2.5 min-h-10">
              <TagInput values={form.positions ?? []} onChange={v => setForm(f => ({ ...f, positions: v }))} placeholder="Add title…" />
            </div>
          </div>
          <div>
            <div className="text-xs text-[var(--color-text-dim)] mb-1.5">Locations</div>
            <div className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg p-2.5 min-h-10">
              <LocationTagInput values={form.locations ?? []} onChange={v => setForm(f => ({ ...f, locations: v }))} />
            </div>
          </div>
        </div>
      </Section>

      <Section title="Blacklists">
        <div className="space-y-4">
          <div>
            <div className="text-xs text-red-400/80 mb-1.5">Blacklisted Companies</div>
            <div className="bg-[var(--color-surface-2)] border border-red-500/20 rounded-lg p-2.5 min-h-10">
              <TagInput values={form.company_blacklist ?? []} onChange={v => setForm(f => ({ ...f, company_blacklist: v }))} placeholder="Add company…" />
            </div>
          </div>
          <div>
            <div className="text-xs text-red-400/80 mb-1.5">Blacklisted Job Titles</div>
            <div className="bg-[var(--color-surface-2)] border border-red-500/20 rounded-lg p-2.5 min-h-10">
              <TagInput values={form.title_blacklist ?? []} onChange={v => setForm(f => ({ ...f, title_blacklist: v }))} placeholder="Add title…" />
            </div>
          </div>
          <div>
            <div className="text-xs text-red-400/80 mb-1.5">Blacklisted Locations</div>
            <div className="bg-[var(--color-surface-2)] border border-red-500/20 rounded-lg p-2.5 min-h-10">
              <TagInput values={form.location_blacklist ?? []} onChange={v => setForm(f => ({ ...f, location_blacklist: v }))} placeholder="Add location…" />
            </div>
          </div>
        </div>
      </Section>

      <Button
        variant="primary"
        fullWidth
        loading={save.isPending}
        leftIcon={saved ? <Check size={14} /> : undefined}
        onClick={() => save.mutate()}
      >
        {saved ? 'Saved' : 'Save Preferences'}
      </Button>
    </div>
  )
}
