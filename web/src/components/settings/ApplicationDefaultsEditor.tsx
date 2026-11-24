import { useEffect, useState, type ReactNode } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import type { ApplicationAnswer, ApplicationDefaults, ApplicationSalaryRange, ApplicationWorkRight } from '../../types'

const controlClass = 'w-full rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-2)] px-3 py-2 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)]'
const gridClass = 'grid grid-cols-1 gap-3 sm:grid-cols-2'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="flex flex-col gap-1 text-xs text-[var(--color-text-dim)]"><span>{label}</span>{children}</label>
}

function TextField({ label, value, onChange, multiline = false, type = 'text', placeholder }: { label: string; value?: string; onChange: (value: string) => void; multiline?: boolean; type?: string; placeholder?: string }) {
  return <Field label={label}>{multiline
    ? <textarea className={controlClass} rows={3} value={value ?? ''} onChange={e => onChange(e.target.value)} placeholder={placeholder} />
    : <input className={controlClass} type={type} value={value ?? ''} onChange={e => onChange(e.target.value)} placeholder={placeholder} />
  }</Field>
}

function BoolField({ label, value, onChange, yes = 'Yes', no = 'No' }: { label: string; value?: boolean | null; onChange: (value: boolean | null) => void; yes?: string; no?: string }) {
  return <Field label={label}><select className={controlClass} value={value == null ? '' : value ? 'yes' : 'no'} onChange={e => onChange(e.target.value === '' ? null : e.target.value === 'yes')}>
    <option value="">Not specified</option><option value="yes">{yes}</option><option value="no">{no}</option>
  </select></Field>
}

function AmountField({ label, value, onChange }: { label: string; value?: number | null; onChange: (value: number | null) => void }) {
  const [draft, setDraft] = useState(value == null ? '' : String(value))
  useEffect(() => {
    if (value == null || Number.isFinite(value)) setDraft(value == null ? '' : String(value))
  }, [value])
  return <Field label={label}><input className={controlClass} inputMode="decimal" value={draft} aria-invalid={value != null && (!Number.isFinite(value) || value < 0)} onChange={e => {
    const next = e.target.value
    setDraft(next)
    // Keep malformed input visible and invalid until corrected. Native number
    // inputs can turn a bad draft into an empty value and silently lose it.
    const numeric = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(next) ? Number(next) : Number.NaN
    onChange(next === '' ? null : numeric)
  }} /></Field>
}

function AddButton({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return <button type="button" className="flex items-center gap-1 text-sm text-[var(--color-accent)] hover:underline" onClick={onClick}><Plus size={14} aria-hidden />{children}</button>
}

function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return <button type="button" aria-label={label} onClick={onClick} className="rounded p-1 text-[var(--color-text-dim)] hover:text-red-400"><Trash2 size={16} aria-hidden /></button>
}

export function ApplicationDefaultsEditor({ value = {}, onChange, errors = [] }: { value?: ApplicationDefaults; onChange: (value: ApplicationDefaults) => void; errors?: string[] }) {
  const salary = value.salary_range ?? {}
  const rights = value.work_rights ?? []
  const relocation = value.relocation ?? {}
  const custom = value.custom_answers ?? []
  function set<K extends keyof ApplicationDefaults>(key: K, next: ApplicationDefaults[K]) { onChange({ ...value, [key]: next }) }
  function setSalary<K extends keyof ApplicationSalaryRange>(key: K, next: ApplicationSalaryRange[K]) { set('salary_range', { ...salary, [key]: next }) }
  function setRight<K extends keyof ApplicationWorkRight>(index: number, key: K, next: ApplicationWorkRight[K]) { set('work_rights', rights.map((right, i) => i === index ? { ...right, [key]: next } : right)) }
  function setCustom<K extends keyof ApplicationAnswer>(index: number, key: K, next: ApplicationAnswer[K]) { set('custom_answers', custom.map((answer, i) => i === index ? { ...answer, [key]: next } : answer)) }

  return <div className="space-y-6 pt-3">
    <p className="text-xs text-[var(--color-text-dim)]">Save answers recruiters commonly ask. Every field is optional; leave unknown facts unspecified. Add country-specific work rights and any other questions below.</p>
    <fieldset className="space-y-3">
      <legend className="mb-2 text-sm font-medium">Availability</legend>
      <div className={gridClass}>
        <TextField label="Notice period" value={value.notice_period} onChange={v => set('notice_period', v)} placeholder="For example, immediately available or 4 weeks" />
        <TextField label="Availability details" value={value.availability} onChange={v => set('availability', v)} placeholder="Hours, commitments or start-date context" />
        <TextField label="Earliest start date" type="date" value={value.earliest_start_date} onChange={v => set('earliest_start_date', v)} />
      </div>
    </fieldset>

    <fieldset className="space-y-3">
      <legend className="mb-2 text-sm font-medium">Salary expectations</legend>
      <TextField label="Salary expectation (free text)" value={value.salary_expectation} onChange={v => set('salary_expectation', v)} placeholder="Your existing answer, including any currency or pay units" />
      <p className="text-xs text-[var(--color-text-dim)]">A structured range makes the currency, pay period and included benefits explicit.</p>
      <div className={gridClass}>
        <AmountField label="Salary minimum" value={salary.minimum} onChange={v => setSalary('minimum', v)} />
        <AmountField label="Salary maximum" value={salary.maximum} onChange={v => setSalary('maximum', v)} />
        <TextField label="Salary currency" value={salary.currency} onChange={v => setSalary('currency', v)} placeholder="For example, AUD" />
        <Field label="Salary pay period"><select className={controlClass} value={salary.pay_period ?? ''} onChange={e => setSalary('pay_period', e.target.value)}>
          <option value="">Not specified</option><option value="hour">Per hour</option><option value="week">Per week</option><option value="month">Per month</option><option value="year">Per year</option>
        </select></Field>
        <Field label="Salary basis"><select className={controlClass} value={salary.basis ?? ''} onChange={e => setSalary('basis', e.target.value)}>
          <option value="">Not specified</option><option value="base">Base pay</option><option value="total">Total compensation</option>
        </select></Field>
        <BoolField label="Benefits / superannuation" value={salary.includes_benefits} onChange={v => setSalary('includes_benefits', v)} yes="Included" no="Excluded" />
        <TextField label="Which benefits?" value={salary.benefits_details} onChange={v => setSalary('benefits_details', v)} placeholder="For example, superannuation" />
        <TextField label="Salary details" value={salary.details} onChange={v => setSalary('details', v)} />
      </div>
    </fieldset>

    <fieldset className="space-y-3">
      <legend className="mb-2 text-sm font-medium">Work rights and sponsorship</legend>
      <BoolField label="General sponsorship requirement" value={value.requires_sponsorship} onChange={v => set('requires_sponsorship', v)} />
      <p className="text-xs text-[var(--color-text-dim)]">Country entries keep your work rights and sponsorship answers specific to each country. Include visa details only when they apply.</p>
      {rights.map((right, index) => <fieldset key={index} className="space-y-3 rounded-lg border border-[var(--color-border)] p-3">
        <legend className="px-1 text-xs">Work rights {index + 1}</legend>
        <div className="flex justify-end"><RemoveButton label={`Remove work rights ${index + 1}`} onClick={() => set('work_rights', rights.filter((_, i) => i !== index))} /></div>
        <div className={gridClass}>
          <TextField label="Country" value={right.country} onChange={v => setRight(index, 'country', v)} />
          <TextField label="Right to work / status" value={right.status} onChange={v => setRight(index, 'status', v)} placeholder="Use your own accurate wording" />
          <BoolField label="Sponsorship required in this country" value={right.requires_sponsorship} onChange={v => setRight(index, 'requires_sponsorship', v)} />
          <TextField label="Visa type or subclass" value={right.visa_type} onChange={v => setRight(index, 'visa_type', v)} />
          <TextField label="Visa expiry date" type="date" value={right.visa_expiry_date} onChange={v => setRight(index, 'visa_expiry_date', v)} />
          <TextField label="Work restrictions" value={right.work_restrictions} onChange={v => setRight(index, 'work_restrictions', v)} />
        </div>
        <TextField label="Work rights details" multiline value={right.details} onChange={v => setRight(index, 'details', v)} />
      </fieldset>)}
      <AddButton onClick={() => set('work_rights', [...rights, {}])}>Add country work rights</AddButton>
    </fieldset>

    <fieldset className="space-y-3">
      <legend className="mb-2 text-sm font-medium">Relocation</legend>
      <BoolField label="Willing to relocate" value={relocation.willing} onChange={v => set('relocation', { ...relocation, willing: v })} />
      <TextField label="Relocation locations (one per line)" multiline value={relocation.locations?.join('\n')} onChange={v => set('relocation', { ...relocation, locations: v.split('\n') })} />
      <TextField label="Relocation details" multiline value={relocation.details} onChange={v => set('relocation', { ...relocation, details: v })} />
    </fieldset>

    <TextField label="Additional application details" multiline value={value.additional_details} onChange={v => set('additional_details', v)} placeholder="Any other facts or context recruiters should know" />
    <fieldset className="space-y-3">
      <legend className="mb-2 text-sm font-medium">Custom questions and answers</legend>
      {custom.map((answer, index) => <fieldset key={index} className="space-y-3 rounded-lg border border-[var(--color-border)] p-3">
        <legend className="px-1 text-xs">Custom answer {index + 1}</legend>
        <div className="flex justify-end"><RemoveButton label={`Remove custom answer ${index + 1}`} onClick={() => set('custom_answers', custom.filter((_, i) => i !== index))} /></div>
        <TextField label="Question" value={answer.question} onChange={v => setCustom(index, 'question', v)} />
        <TextField label="Answer" multiline value={answer.answer} onChange={v => setCustom(index, 'answer', v)} />
        <TextField label="Country scope (optional)" value={answer.country} onChange={v => setCustom(index, 'country', v)} />
      </fieldset>)}
      <AddButton onClick={() => set('custom_answers', [...custom, {}])}>Add custom question</AddButton>
    </fieldset>
    {errors.length > 0 && <div role="alert" className="rounded-lg border border-red-500/30 bg-red-500/5 p-3 text-sm text-red-400"><ul className="list-disc space-y-1 pl-4">{errors.map(error => <li key={error}>{error}</li>)}</ul></div>}
  </div>
}
