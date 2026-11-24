import { describe, expect, it } from 'vitest'
import { mergeApplicationDefaults, validateApplicationDefaults } from './applicationDefaults'

describe('application defaults', () => {
  it('preserves candidate facts and reviewed answers when importing extracted facts', () => {
    const existing = { notice_period: '3 weeks', requires_sponsorship: false, form_answers: { retained: 'Original' }, work_rights: [{ country: 'Australia', status: 'Permanent resident' }] }
    const extracted = { notice_period: 'Immediately available', requires_sponsorship: true, availability: 'Weekdays', form_answers: { retained: 'Changed', new: 'Imported' } }
    expect(mergeApplicationDefaults(existing, extracted)).toEqual({ ...existing, availability: 'Weekdays', form_answers: { retained: 'Original', new: 'Imported' } })
    expect(mergeApplicationDefaults(undefined, undefined)).toBeUndefined()
  })

  it('rejects malformed numeric facts and dates while permitting unknown optional facts', () => {
    expect(validateApplicationDefaults({})).toEqual([])
    expect(validateApplicationDefaults({ salary_expectation: 'Negotiable', requires_sponsorship: null, relocation: { willing: null } })).toEqual([])
    for (const amount of [-1, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(validateApplicationDefaults({ salary_range: { minimum: amount } })).toContain('Salary amounts must be finite numbers of zero or more.')
    }
    expect(validateApplicationDefaults({ earliest_start_date: '2026-02-30' })).toHaveLength(1)
    expect(validateApplicationDefaults({ work_rights: [{ country: 'Australia', visa_expiry_date: '2026-13-01' }] })).toHaveLength(1)
    expect(validateApplicationDefaults({ salary_range: { minimum: 70000, currency: 'AUD', pay_period: 'year', basis: 'base', includes_benefits: null } })).toEqual([])
  })
})
