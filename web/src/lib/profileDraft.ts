import type { ResumeProfile } from '../types'
import { mergeApplicationDefaults } from './applicationDefaults'

// A document can finish processing after the candidate has continued editing.
// Only fields unchanged since upload started may accept the extracted value.
export function mergeExtractedProfile(current: ResumeProfile, started: ResumeProfile, extracted: ResumeProfile): ResumeProfile {
  const next: ResumeProfile = { ...current }
  for (const key of Object.keys(extracted) as (keyof ResumeProfile)[]) {
    if (key === 'personal_information' || key === 'application_defaults') continue
    if (JSON.stringify(current[key]) === JSON.stringify(started[key])) Object.assign(next, { [key]: extracted[key] })
  }
  next.personal_information = { ...current.personal_information }
  for (const [key, value] of Object.entries(extracted.personal_information ?? {})) {
    if (current.personal_information?.[key as keyof NonNullable<ResumeProfile['personal_information']>] === started.personal_information?.[key as keyof NonNullable<ResumeProfile['personal_information']>]) Object.assign(next.personal_information, { [key]: value })
  }
  next.application_defaults = mergeApplicationDefaults(current.application_defaults, extracted.application_defaults)
  return next
}
