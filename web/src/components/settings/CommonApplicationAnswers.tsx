import { useEffect, useState, type ReactNode } from 'react'
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import { cn } from '../../lib'
import type { ApplicationDefaults, ApplicationSalaryRange, ApplicationWorkRight } from '../../types'
import type { ApplicationAnswerSource, ApplicationAnswerTopic } from '../../api/settings'
import { TextField, BoolField, AmountField } from './ApplicationDefaultsEditor'

const controls = 'w-full min-h-11 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-2)] px-3 py-2 text-sm text-[var(--color-text)] outline-none focus:border-[var(--color-accent)] focus:ring-2 focus:ring-[var(--color-accent)]/20'
const grid = 'grid grid-cols-1 gap-3 sm:grid-cols-2'
const action = 'min-h-11 rounded-lg px-2 text-sm text-[var(--color-accent)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-accent)] hover:underline'
const core = [{ id: 'work_rights', label: 'Right to work' }, { id: 'sponsorship', label: 'Sponsorship' }, { id: 'notice_period', label: 'Notice period' }, { id: 'earliest_start_date', label: 'Earliest start' }, { id: 'salary', label: 'Expected pay' }]
const optional = [{ id: 'relocation', label: 'Relocation' }, { id: 'travel', label: 'Travel' }, { id: 'work_arrangement', label: 'Work arrangement and commute' }, { id: 'working_hours', label: 'Working hours' }, { id: 'credentials', label: 'Relevant credentials' }]

function sourceLabel(source?: ApplicationAnswerSource) {
  if (source?.label) return source.label
  return { cv: 'From CV', profile: 'From profile', preferences: 'From preferences', candidate: 'Saved', confirmed: 'Confirmed' }[source?.source ?? 'candidate']
}
function filledSummary(id: string, value: ApplicationDefaults, catalog: ApplicationAnswerTopic[]): string {
  const right = value.work_rights?.filter(r => r.status?.trim()).map(r => [r.country, r.status].filter(Boolean).join(': ')).join('; ')
  const salary = value.salary_range
  switch (id) {
    case 'work_rights': return right || ''
    case 'sponsorship': return [value.requires_sponsorship == null ? '' : value.requires_sponsorship ? 'Required now' : 'Not required now', value.future_requires_sponsorship == null ? '' : value.future_requires_sponsorship ? 'Required in future' : 'Not required in future'].filter(Boolean).join('; ')
    case 'notice_period': return value.notice_period || value.availability || ''
    case 'earliest_start_date': return value.earliest_start_date || ''
    case 'salary': return value.salary_expectation || (salary && (salary.minimum != null || salary.maximum != null) && [salary.minimum, salary.maximum].every(amount => amount == null || Number.isFinite(amount)) ? [salary.currency, salary.minimum != null && salary.maximum != null ? `${salary.minimum.toLocaleString()}–${salary.maximum.toLocaleString()}` : (salary.minimum ?? salary.maximum)?.toLocaleString(), salary.pay_period ? `per ${salary.pay_period}` : '', salary.basis === 'base' ? 'base pay' : salary.basis === 'total' ? 'total compensation' : ''].filter(Boolean).join(' ') : '')
    case 'relocation': return value.relocation?.willing == null ? value.relocation?.details || '' : value.relocation.willing ? ['Open to relocation', value.relocation.locations?.filter(Boolean).join(', ')].filter(Boolean).join(': ') : 'Not open to relocation'
    case 'travel': return value.travel_conditions || ''
    case 'working_hours': return value.working_hours || ''
    case 'work_arrangement': return value.commute_conditions || catalog.find(t => t.id === id)?.summary || ''
    case 'credentials': return catalog.find(t => t.id === id)?.summary || ''
    default: return ''
  }
}

function Row({ id, label, summary, source, open, onOpen, children }: { id: string; label: string; summary: string; source?: ApplicationAnswerSource; open: boolean; onOpen: () => void; children: ReactNode }) {
  return <div className="border-t border-[var(--color-border)]">
    <button type="button" aria-label={`${label}: ${summary || 'Not specified'}. ${summary ? sourceLabel(source) : 'Add answer'}`} aria-expanded={open} aria-controls={`answer-editor-${id}`} onClick={onOpen} className="grid min-h-16 w-full grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-center gap-x-3 gap-y-1 py-3 text-left focus-visible:outline-2 focus-visible:outline-[var(--color-accent)] sm:grid-cols-[minmax(130px,1fr)_minmax(0,1.6fr)_auto_16px]">
      <span className="text-sm font-medium">{label}</span>
      <span className={cn('col-span-2 col-start-1 row-start-2 break-words text-sm sm:col-span-1 sm:col-start-auto sm:row-start-auto', !summary && 'text-[var(--color-text-dim)]')}>{summary || 'Not specified'}</span>
      <span className={cn('col-start-2 row-start-1 break-words text-right text-xs sm:col-start-auto sm:row-start-auto sm:text-left', summary ? 'text-[var(--color-text-dim)]' : 'text-[var(--color-accent)]')}>{summary ? sourceLabel(source) : 'Add answer'}</span>
      <ChevronDown size={16} aria-hidden className={cn('hidden text-[var(--color-text-dim)] sm:block motion-safe:transition-transform', open && 'rotate-180')} />
    </button>
    <div id={`answer-editor-${id}`} hidden={!open} className="space-y-3 pb-5">{source?.evidence?.length ? <ul className="space-y-1 text-xs text-[var(--color-text-dim)]">{source.evidence.map((e, i) => <li key={i}>{e}</li>)}</ul> : null}{children}</div>
  </div>
}

export function CommonApplicationAnswers({ value, onChange, onTopicChange, sources = {}, catalog = [], onEditProfile, openTopic }: { value: ApplicationDefaults; onChange: (value: ApplicationDefaults) => void; onTopicChange: (topic: string) => void; sources?: Record<string, ApplicationAnswerSource>; catalog?: ApplicationAnswerTopic[]; onEditProfile: () => void; openTopic?: { id: string; request: number } }) {
  const [active, setActive] = useState<string | null>(null)
  const [more, setMore] = useState(false)
  const [custom, setCustom] = useState(false)
  useEffect(() => {
    if (!openTopic) return
    if (openTopic.id === 'additional_answers') setCustom(true)
    else {
      setActive(openTopic.id)
      if (optional.some(topic => topic.id === openTopic.id)) setMore(true)
    }
  }, [openTopic])
  const salary = value.salary_range ?? {}
  const rights = value.work_rights ?? []
  function change(topic: string, patch: Partial<ApplicationDefaults>) { onTopicChange(topic); onChange({ ...value, ...patch }) }
  function setSalary<K extends keyof ApplicationSalaryRange>(key: K, v: ApplicationSalaryRange[K]) { change('salary', { salary_range: { ...salary, [key]: v } }) }
  function setRight<K extends keyof ApplicationWorkRight>(i: number, key: K, v: ApplicationWorkRight[K]) { change('work_rights', { work_rights: rights.map((r, j) => i === j ? { ...r, [key]: v } : r) }) }
  const editors: Record<string, ReactNode> = {
    work_rights: <>
      <p className="text-xs text-[var(--color-text-dim)]">Keep work rights specific to each country. Leave unknown facts unspecified.</p>
      {rights.map((right, i) => <fieldset key={i} className="space-y-3 rounded-lg border border-[var(--color-border)] p-3">
        <legend className="px-1 text-xs">Work rights {i + 1}</legend>
        <div className="flex justify-end"><button type="button" className={action} aria-label={`Remove work rights ${i + 1}`} onClick={() => change('work_rights', { work_rights: rights.filter((_, j) => i !== j) })}><Trash2 size={16} aria-hidden /></button></div>
        <div className={grid}>
          <TextField label="Country" value={right.country} onChange={v => setRight(i, 'country', v)} />
          <TextField label="Right to work / status" value={right.status} onChange={v => setRight(i, 'status', v)} />
          <BoolField label="Sponsorship required in this country" value={right.requires_sponsorship} onChange={v => setRight(i, 'requires_sponsorship', v)} />
          <BoolField label="Future sponsorship required in this country" value={right.future_requires_sponsorship} onChange={v => setRight(i, 'future_requires_sponsorship', v)} />
          <TextField label="Visa type or subclass" value={right.visa_type} onChange={v => setRight(i, 'visa_type', v)} />
          <TextField label="Visa expiry date" type="date" value={right.visa_expiry_date} onChange={v => setRight(i, 'visa_expiry_date', v)} />
          <TextField label="Work restrictions" value={right.work_restrictions} onChange={v => setRight(i, 'work_restrictions', v)} />
        </div>
        <TextField label="Work rights details" multiline value={right.details} onChange={v => setRight(i, 'details', v)} />
      </fieldset>)}
      <button type="button" className={cn(action, 'inline-flex items-center gap-1')} onClick={() => change('work_rights', { work_rights: [...rights, {}] })}><Plus size={14} aria-hidden />Add country work rights</button>
    </>,
    sponsorship: <><BoolField label="General sponsorship requirement" value={value.requires_sponsorship} onChange={v => change('sponsorship', { requires_sponsorship: v })} /><BoolField label="Future sponsorship requirement" value={value.future_requires_sponsorship} onChange={v => change('sponsorship', { future_requires_sponsorship: v })} /><p className="text-xs text-[var(--color-text-dim)]">Use Right to work for country-specific sponsorship conditions.</p></>,
    notice_period: <div className={grid}><TextField label="Notice period" value={value.notice_period} onChange={v => change('notice_period', { notice_period: v })} placeholder="For example, 4 weeks" /><TextField label="Availability details" value={value.availability} onChange={v => change('notice_period', { availability: v })} /></div>,
    earliest_start_date: <TextField label="Earliest start date" type="date" value={value.earliest_start_date} onChange={v => change('earliest_start_date', { earliest_start_date: v })} />,
    salary: <>
      <TextField label="Salary expectation (free text)" value={value.salary_expectation} onChange={v => change('salary', { salary_expectation: v })} />
      <p className="text-xs text-[var(--color-text-dim)]">Specify currency and pay period when giving a range.</p>
      <div className={grid}>
        <AmountField label="Salary minimum" value={salary.minimum} onChange={v => setSalary('minimum', v)} /><AmountField label="Salary maximum" value={salary.maximum} onChange={v => setSalary('maximum', v)} />
        <TextField label="Salary currency" value={salary.currency} onChange={v => setSalary('currency', v)} placeholder="For example, AUD" />
        <label className="space-y-1 text-xs text-[var(--color-text-dim)]">Salary pay period<select className={controls} value={salary.pay_period ?? ''} onChange={e => setSalary('pay_period', e.target.value)}><option value="">Not specified</option>{['hour', 'week', 'month', 'year'].map(period => <option key={period} value={period}>Per {period}</option>)}</select></label>
        <label className="space-y-1 text-xs text-[var(--color-text-dim)]">Salary basis<select className={controls} value={salary.basis ?? ''} onChange={e => setSalary('basis', e.target.value)}><option value="">Not specified</option><option value="base">Base pay</option><option value="total">Total compensation</option></select></label>
        <BoolField label="Benefits / superannuation" value={salary.includes_benefits} onChange={v => setSalary('includes_benefits', v)} yes="Included" no="Excluded" />
        <TextField label="Which benefits?" value={salary.benefits_details} onChange={v => setSalary('benefits_details', v)} /><TextField label="Salary details" value={salary.details} onChange={v => setSalary('details', v)} />
      </div>
    </>,
    relocation: <><BoolField label="Willing to relocate" value={value.relocation?.willing} onChange={v => change('relocation', { relocation: { ...value.relocation, willing: v } })} /><TextField label="Relocation locations (one per line)" multiline value={value.relocation?.locations?.join('\n')} onChange={v => change('relocation', { relocation: { ...value.relocation, locations: v.split('\n') } })} /><TextField label="Relocation details" multiline value={value.relocation?.details} onChange={v => change('relocation', { relocation: { ...value.relocation, details: v } })} /></>,
    travel: <TextField label="Travel conditions" multiline value={value.travel_conditions} onChange={v => change('travel', { travel_conditions: v })} placeholder="How often, where and any notice you need" />,
    working_hours: <TextField label="Working hours" multiline value={value.working_hours} onChange={v => change('working_hours', { working_hours: v })} placeholder="Days, shifts and any conditions" />,
    work_arrangement: <><TextField label="Commute conditions" multiline value={value.commute_conditions} onChange={v => change('work_arrangement', { commute_conditions: v })} /><Link to="/settings/preferences" className={cn(action, 'inline-flex items-center')}>Edit work preferences</Link></>,
    credentials: <><p className="text-sm text-[var(--color-text-dim)]">{catalog.find(t => t.id === 'credentials')?.summary || 'Add relevant qualifications and certifications to your profile.'}</p><button type="button" className={action} onClick={onEditProfile}>Edit credentials in CV &amp; profile</button></>,
  }
  function renderRow(topic: { id: string; label: string }) {
    return <Row key={topic.id} {...topic} summary={filledSummary(topic.id, value, catalog)} source={sources[topic.id]} open={active === topic.id} onOpen={() => setActive(current => current === topic.id ? null : topic.id)}>{editors[topic.id]}</Row>
  }
  const filled = core.filter(t => filledSummary(t.id, value, catalog)).length
  return <div className="space-y-4">
    <section aria-label="Common answers" className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)]">
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-4 sm:px-5"><h3 className="text-sm font-semibold">Common answers</h3><span className="text-xs text-[var(--color-text-dim)]">{filled} of 5 filled</span></div>
      <div className="px-4 sm:px-5">{core.map(renderRow)}</div>
    </section>
    <section>
      <button type="button" aria-expanded={more} aria-controls="other-answers" onClick={() => setMore(v => !v)} className="flex min-h-11 w-full items-center justify-between gap-2 text-left text-sm font-medium focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"><span>Other common questions <span className="ml-2 text-xs font-normal text-[var(--color-text-dim)]">5 optional</span></span><ChevronDown size={16} aria-hidden className={cn(more && 'rotate-180')} /></button>
      <div id="other-answers" hidden={!more}>{optional.map(renderRow)}</div>
    </section>
    <section>
      <button type="button" aria-expanded={custom} aria-controls="custom-answers" onClick={() => setCustom(v => !v)} className="flex min-h-11 w-full items-center justify-between gap-2 border-t border-[var(--color-border)] text-left text-sm font-medium focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"><span>My additional answers {value.custom_answers?.length ? <span className="ml-2 text-xs font-normal text-[var(--color-text-dim)]">{value.custom_answers.length} saved</span> : null}</span><ChevronDown size={16} aria-hidden className={cn(custom && 'rotate-180')} /></button>
      <div id="custom-answers" hidden={!custom} className="space-y-4 pt-3">
        {(value.custom_answers ?? []).map((answer, i) => <fieldset key={i} className="space-y-3 border-t border-[var(--color-border)] pt-3"><legend className="text-xs">Custom answer {i + 1}</legend><div className="flex justify-end"><button type="button" className={action} aria-label={`Remove custom answer ${i + 1}`} onClick={() => change('additional_answers', { custom_answers: value.custom_answers?.filter((_, j) => i !== j) })}><Trash2 size={16} aria-hidden /></button></div>{(['question', 'answer', 'country'] as const).map(key => <TextField key={key} label={{ question: 'Question', answer: 'Answer', country: 'Country scope (optional)' }[key]} multiline={key === 'answer'} value={answer[key]} onChange={v => change('additional_answers', { custom_answers: value.custom_answers?.map((a, j) => i === j ? { ...a, [key]: v } : a) })} />)}</fieldset>)}
        <button type="button" className={cn(action, 'inline-flex items-center gap-1')} onClick={() => change('additional_answers', { custom_answers: [...(value.custom_answers ?? []), {}] })}><Plus size={14} aria-hidden />Add custom question</button>
        <TextField label="Additional application details" multiline value={value.additional_details} onChange={v => change('additional_answers', { additional_details: v })} />
      </div>
    </section>
  </div>
}
