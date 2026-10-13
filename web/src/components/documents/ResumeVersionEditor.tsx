import { useState } from 'react'
import { inputClassName } from '../ui/input'
import type { ExperienceDetail, ResumeProfile } from '../../types'

function TextListField({ label, values, onChange }: {
  label: string
  values?: string[]
  onChange: (values: string[]) => void
}) {
  // Keep the raw draft locally so typing a newline doesn't lose the empty line.
  const [text, setText] = useState(() => (values ?? []).join('\n'))
  return (
    <label className="block text-sm">
      {label} <span className="text-[var(--color-text-dim)]">(one per line)</span>
      <textarea
        className={inputClassName + ' min-h-[100px] mt-1'}
        value={text}
        onChange={e => {
          setText(e.target.value)
          onChange(e.target.value.split('\n').map(line => line.trim()).filter(Boolean))
        }}
      />
    </label>
  )
}

export function ResumeVersionEditor({ profile, onChange }: {
  profile: ResumeProfile
  onChange: (profile: ResumeProfile) => void
}) {
  function updateExperience(index: number, patch: Partial<ExperienceDetail>) {
    onChange({
      ...profile,
      experience_details: profile.experience_details?.map((role, i) => i === index ? { ...role, ...patch } : role),
    })
  }

  return (
    <div className="space-y-4">
      <label className="block text-sm">
        Summary
        <textarea
          className={inputClassName + ' min-h-[120px] mt-1'}
          value={profile.summary ?? ''}
          onChange={e => onChange({ ...profile, summary: e.target.value })}
        />
      </label>
      <TextListField label="Skills" values={profile.skills} onChange={skills => onChange({ ...profile, skills })} />
      {(profile.experience_details ?? []).map((role, index) => (
        <fieldset key={index} className="border border-[var(--color-border)] rounded-lg p-3 space-y-3">
          <legend className="text-sm font-medium px-1">Experience {index + 1}{role.company ? ` — ${role.company}` : ''}</legend>
          {(['position', 'company', 'employment_period', 'location'] as const).map(field => (
            <label key={field} className="block text-sm">
              {{ position: 'Job title', company: 'Employer', employment_period: 'Employment dates', location: 'Location' }[field]}
              <input className={inputClassName + ' mt-1'} value={role[field] ?? ''}
                onChange={e => updateExperience(index, { [field]: e.target.value })} />
            </label>
          ))}
          <TextListField label="Responsibilities and achievements" values={role.key_responsibilities}
            onChange={key_responsibilities => updateExperience(index, { key_responsibilities })} />
          <TextListField label="Skills acquired" values={role.skills_acquired}
            onChange={skills_acquired => updateExperience(index, { skills_acquired })} />
        </fieldset>
      ))}
    </div>
  )
}
