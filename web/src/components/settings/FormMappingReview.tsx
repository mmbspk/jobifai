import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import type { FormMappingReview as Mapping } from '../../api/settings'
import type { ResumeProfile } from '../../types'
import { Button } from '../Button'
import { inputClassName } from '../ui/input'
import { SettingsSection } from './settings-ui'

const profileFields: Record<string, string> = {
  'personal_information.full_name': 'Full name',
  'personal_information.name': 'First name',
  'personal_information.surname': 'Last name',
  'personal_information.email': 'Email address',
  'personal_information.phone': 'Phone number',
  'personal_information.country': 'Country',
  'personal_information.city': 'City',
  'personal_information.zip_code': 'Postcode',
  'personal_information.address': 'Address',
  'application_defaults.notice_period': 'Notice period',
  'application_defaults.salary_expectation': 'Salary expectation',
  'application_defaults.requires_sponsorship': 'Requires sponsorship',
  'application_defaults.availability': 'Availability',
  'application_defaults.earliest_start_date': 'Earliest start date',
  'application_defaults.additional_details': 'Additional application details',
}

function savedAnswer(binding: string, profile?: ResumeProfile): string {
  if (!profile) return ''
  const personal = profile.personal_information ?? {}
  const defaults = profile.application_defaults
  if (binding.startsWith('application_defaults.form_answers.')) return defaults?.form_answers?.[binding.slice('application_defaults.form_answers.'.length)] ?? ''
  const values: Record<string, string | undefined> = {
    'personal_information.full_name': [personal.name, personal.surname].filter(Boolean).join(' '),
    'personal_information.name': personal.name,
    'personal_information.surname': personal.surname,
    'personal_information.email': personal.email,
    'personal_information.phone': personal.phone,
    'personal_information.country': personal.country,
    'personal_information.city': personal.city,
    'personal_information.zip_code': personal.zip_code,
    'personal_information.address': personal.address,
    'application_defaults.notice_period': defaults?.notice_period,
    'application_defaults.salary_expectation': defaults?.salary_expectation,
    'application_defaults.requires_sponsorship': defaults?.requires_sponsorship === undefined ? '' : defaults.requires_sponsorship ? 'Yes' : 'No',
    'application_defaults.availability': defaults?.availability,
    'application_defaults.earliest_start_date': defaults?.earliest_start_date,
    'application_defaults.additional_details': defaults?.additional_details,
  }
  return values[binding] ?? ''
}

function MappingRow({ item, profile }: { item: Mapping; profile?: ResumeProfile }) {
  const qc = useQueryClient()
  const [binding, setBinding] = useState(item.binding_key)
  const [answer, setAnswer] = useState('')
  const [confirmedAnswer, setConfirmedAnswer] = useState<string | null>(null)
  const [editing, setEditing] = useState(item.provenance !== 'verified_by_model')
  const currentAnswer = binding ? savedAnswer(binding, profile) : answer
  const confirmed = confirmedAnswer !== null && confirmedAnswer === currentAnswer
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['form-mappings'] })
    void qc.invalidateQueries({ queryKey: ['settings-resume'] })
  }
  const save = useMutation({ mutationFn: () => settingsApi.formMappings.confirm(item.id, { confirmed: true, binding_key: binding || undefined, answer: binding ? undefined : answer }), onSuccess: refresh })
  const invalidate = useMutation({ mutationFn: () => settingsApi.formMappings.invalidate(item.id), onSuccess: refresh })
  const trusted = item.provenance === 'confirmed_by_reviewer' || item.provenance === 'trusted_reference' || item.provenance === 'verified_by_model'
  if (!editing) return <div className="space-y-3 rounded-lg border border-[var(--color-border)] p-4">
    <p className="font-medium">{item.field.label}</p>
    <p className="text-sm text-[var(--color-text-dim)]">{item.platform} · Verified automatically</p>
    {item.field.job_title ? <p className="text-sm">Applies to: {item.field.job_title}{item.field.job_location ? ' · ' + item.field.job_location : ''}</p> : null}
    <p className="text-sm">Answered from your profile. Reuse is checked against your current information and this question's context.</p>
    <div className="flex gap-2">
      <Button variant="secondary" onClick={() => setEditing(true)}>Review or override</Button>
      <Button variant="secondary" loading={invalidate.isPending} onClick={() => invalidate.mutate()}>Disable mapping</Button>
    </div>
    {invalidate.isError ? <p role="alert">{invalidate.error?.message ?? 'Could not disable this mapping.'}</p> : null}
  </div>
  return <div className="space-y-3 rounded-lg border border-[var(--color-border)] p-4">
    <p className="font-medium">{item.field.label}</p>
    <p className="text-sm text-[var(--color-text-dim)]">{item.platform} · {item.employer_key || 'General question'}{item.field.group ? ' · ' + item.field.group : ''} · {trusted ? 'Verified' : item.provenance}</p>
    {item.field.help_text ? <p className="text-sm">{item.field.help_text}</p> : null}
    {item.field.job_title ? <p className="text-sm">Applies to: {item.field.job_title}{item.field.job_location ? ' · ' + item.field.job_location : ''}</p> : null}
    {item.field.max_chars ? <p className="text-sm">Maximum {item.field.max_chars} characters</p> : null}
    {item.field.min_value || item.field.max_value ? <p className="text-sm">Allowed range: {item.field.min_value || 'no minimum'} to {item.field.max_value || 'no maximum'}</p> : null}
    {item.field.options?.length ? <p className="text-sm">Options: {item.field.options.join(', ')}</p> : null}
    <label className="block text-sm">Use answer from
      <select aria-label={'Answer source for ' + item.field.label} className={inputClassName} value={binding} onChange={e => { setBinding(e.target.value); setConfirmedAnswer(null) }}>
        <option value="">Provide my reviewed answer</option>
        {binding && !profileFields[binding] ? <option value={binding}>My saved reviewed answer</option> : null}
        {Object.entries(profileFields).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
      </select>
    </label>
    {binding ? <p className="text-sm">Current saved answer: {currentAnswer || 'Missing — update your profile or provide a reviewed answer.'}</p> : null}
    {!binding ? <label className="block text-sm">Your reviewed answer
      <input aria-label={'Answer for ' + item.field.label} className={inputClassName} value={answer} onChange={e => { setAnswer(e.target.value); setConfirmedAnswer(null) }} maxLength={2000} />
    </label> : null}
    <label className="flex items-start gap-2 text-sm">
      <input type="checkbox" checked={confirmed} onChange={e => setConfirmedAnswer(e.target.checked ? currentAnswer : null)} />
      I confirm this question's meaning and that the selected profile field or answer is correct for me.
    </label>
    {save.isError || invalidate.isError ? <p role="alert">{(save.error ?? invalidate.error)?.message ?? 'Could not update this mapping.'}</p> : null}
    <div className="flex gap-2">
      <Button disabled={!confirmed || !currentAnswer.trim()} loading={save.isPending} onClick={() => save.mutate()}>Confirm question mapping</Button>
      {trusted ? <Button variant="secondary" loading={invalidate.isPending} onClick={() => invalidate.mutate()}>Disable mapping</Button> : null}
    </div>
  </div>
}

export function FormMappingReview() {
  const query = useQuery({ queryKey: ['form-mappings'], queryFn: settingsApi.formMappings.list })
  const profile = useQuery({ queryKey: ['settings-resume'], queryFn: settingsApi.resume.get })
  return <SettingsSection title="Application question mappings">
    <p className="text-sm text-[var(--color-text-dim)]">Answers supported by your profile are verified and learned automatically. You can review missing information, override an answer or disable reuse here.</p>
    {query.isError ? <p role="alert">Could not load application questions.</p> : null}
    {query.isLoading ? <p>Loading questions…</p> : null}
    {profile.isError ? <p role="alert">Could not load saved profile answers.</p> : null}
    {query.data?.entries.length === 0 ? <p>No application questions need review yet.</p> : null}
    {query.data?.entries.map(item => <MappingRow key={item.id} item={item} profile={profile.data} />)}
  </SettingsSection>
}
