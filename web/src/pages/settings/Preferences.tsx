import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { TagInput } from '../../components/TagInput'
import { SearchTargetList } from '../../components/SearchTargetList'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsSection } from '../../components/settings/settings-ui'
import type { SearchTarget, WorkPreferences } from '../../types'
import { cn } from '../../lib'

function Radio({ label, checked, onChange }: { label: string; checked: boolean; onChange: () => void }) {
  return (
    <label className="flex items-center gap-2.5 cursor-pointer select-none min-h-[44px] sm:min-h-0">
      <input type="radio" checked={checked} onChange={onChange} className="accent-[var(--color-accent)]" />
      <span className="text-sm text-[var(--color-text-muted)]">{label}</span>
    </label>
  )
}

const DATE_FILTER_KEYS = ['all_time', 'month', 'week', 'hours_24'] as const
type DateFilterKey = typeof DATE_FILTER_KEYS[number]

function pickDateFilter(d: WorkPreferences['date_filters']): DateFilterKey {
  if (d?.hours_24) return 'hours_24'
  if (d?.week) return 'week'
  if (d?.month) return 'month'
  if (d?.all_time) return 'all_time'
  return 'week'
}

function dateFilterRecord(key: DateFilterKey): WorkPreferences['date_filters'] {
  return {
    all_time: key === 'all_time',
    month: key === 'month',
    week: key === 'week',
    hours_24: key === 'hours_24',
  }
}

function Checkbox({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-2.5 cursor-pointer select-none min-h-[44px] sm:min-h-0">
      <button
        type="button"
        role="checkbox"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={cn(
          'w-4 h-4 rounded border flex items-center justify-center transition-colors shrink-0',
          checked ? 'bg-[var(--color-accent)] border-[var(--color-accent)]' : 'border-[var(--color-border)]',
        )}
      >
        {checked && <Check size={10} strokeWidth={3} className="text-white" />}
      </button>
      <span className="text-sm text-[var(--color-text-muted)]">{label}</span>
    </label>
  )
}

const DEFAULT_TARGET: SearchTarget = { location: '', remote: true, hybrid: true, onsite: false }

const DEFAULT: WorkPreferences = {
  experience_level: { entry: true, associate: true, mid_senior_level: true, senior: true },
  job_types: { full_time: true },
  date_filters: { week: true },
  positions: [],
  search_targets: [DEFAULT_TARGET],
  company_blacklist: [], title_blacklist: [], location_blacklist: [],
}

function normalizePrefs(p: WorkPreferences): WorkPreferences {
  const dateKey = pickDateFilter(p.date_filters)
  const withDate = { ...p, date_filters: dateFilterRecord(dateKey) }
  if (withDate.search_targets?.length) return withDate
  if (withDate.locations?.length) {
    return {
      ...withDate,
      search_targets: withDate.locations.map(loc => ({
        location: loc,
        remote: withDate.remote ?? false,
        hybrid: withDate.hybrid ?? false,
        onsite: withDate.onsite ?? false,
      })),
    }
  }
  return {
    ...withDate,
    search_targets: [{
      location: '',
      remote: withDate.remote ?? true,
      hybrid: withDate.hybrid ?? true,
      onsite: withDate.onsite ?? false,
    }],
  }
}

function formatLabel(key: string): string {
  return key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

export function Preferences() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings-preferences'], queryFn: settingsApi.preferences.get })
  const [form, setForm] = useState<WorkPreferences>(DEFAULT)
  const [saved, setSaved] = useState(false)

  useEffect(() => { if (data) setForm(normalizePrefs(data)) }, [data])

  const save = useMutation({
    mutationFn: () => settingsApi.preferences.set(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settings-preferences'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  function toggle<K extends 'experience_level' | 'job_types'>(section: K, key: string) {
    setForm(f => ({ ...f, [section]: { ...f[section], [key]: !((f[section] as Record<string, boolean>)[key]) } }))
  }

  const selectedDateFilter = pickDateFilter(form.date_filters)
  const targets = form.search_targets?.length ? form.search_targets : [DEFAULT_TARGET]

  return (
    <div className="space-y-6">
      <PageHeader
        title="Preferences"
        description="Target roles, locations, and filters used when searching job boards."
      />

      <SettingsSection title="Target roles">
        <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] p-2.5 min-h-11">
          <TagInput values={form.positions ?? []} onChange={v => setForm(f => ({ ...f, positions: v }))} placeholder="Add target title…" />
        </div>
      </SettingsSection>

      <SettingsSection title="Search targets" description="Location searches and work arrangements per region.">
        <p className="text-xs text-[var(--color-text-dim)] -mt-2 mb-2">Location searches</p>
        <SearchTargetList targets={targets} onChange={search_targets => setForm(f => ({ ...f, search_targets }))} />
      </SettingsSection>

      <SettingsSection title="Experience level" description="Filters LinkedIn search (Seek matches by title after search).">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
          {(['internship', 'entry', 'associate', 'mid_senior_level', 'senior', 'director', 'executive'] as const).map(k => (
            <Checkbox
              key={k}
              label={formatLabel(k)}
              checked={(form.experience_level as Record<string, boolean>)?.[k] ?? false}
              onChange={() => toggle('experience_level', k)}
            />
          ))}
        </div>
      </SettingsSection>

      <SettingsSection title="Job types">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
          {(['full_time', 'contract', 'part_time', 'temporary', 'internship', 'volunteer', 'other'] as const).map(k => (
            <Checkbox
              key={k}
              label={formatLabel(k)}
              checked={(form.job_types as Record<string, boolean>)?.[k] ?? false}
              onChange={() => toggle('job_types', k)}
            />
          ))}
        </div>
      </SettingsSection>

      <SettingsSection title="Listing age">
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          {DATE_FILTER_KEYS.map(k => (
            <Radio
              key={k}
              label={formatLabel(k)}
              checked={selectedDateFilter === k}
              onChange={() => setForm(f => ({ ...f, date_filters: dateFilterRecord(k) }))}
            />
          ))}
        </div>
      </SettingsSection>

      <SettingsSection title="Blacklists">
        <div className="space-y-4">
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-1.5">Companies</p>
            <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] p-2.5 min-h-10">
              <TagInput values={form.company_blacklist ?? []} onChange={v => setForm(f => ({ ...f, company_blacklist: v }))} placeholder="Add company…" />
            </div>
          </div>
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-1.5">Titles</p>
            <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] p-2.5 min-h-10">
              <TagInput values={form.title_blacklist ?? []} onChange={v => setForm(f => ({ ...f, title_blacklist: v }))} placeholder="Add title…" />
            </div>
          </div>
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-1.5">Locations</p>
            <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)] p-2.5 min-h-10">
              <TagInput values={form.location_blacklist ?? []} onChange={v => setForm(f => ({ ...f, location_blacklist: v }))} placeholder="Add location…" />
            </div>
          </div>
        </div>
      </SettingsSection>

      <Button variant="primary" fullWidth loading={save.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => save.mutate()}>
        {saved ? 'Saved' : 'Save changes'}
      </Button>
    </div>
  )
}
