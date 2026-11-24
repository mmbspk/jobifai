import type { ApplicationDefaults } from '../types'

function validDate(value: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
}

export function validateApplicationDefaults(value: ApplicationDefaults = {}): string[] {
  const errors: string[] = []
  if (value.earliest_start_date && !validDate(value.earliest_start_date)) errors.push('Earliest start date must use a valid YYYY-MM-DD date.')
  const salary = value.salary_range
  if (salary) {
    if ([salary.minimum, salary.maximum].some(amount => amount != null && (!Number.isFinite(amount) || amount < 0))) errors.push('Salary amounts must be finite numbers of zero or more.')
    if (salary.minimum != null && salary.maximum != null && salary.minimum > salary.maximum) errors.push('Salary minimum must not exceed the maximum.')
    if (salary.minimum != null || salary.maximum != null) {
      if (!/^[a-z]{3}$/i.test(salary.currency?.trim() ?? '')) errors.push('Salary currency must be a three-letter currency code.')
      if (!['hour', 'week', 'month', 'year'].includes(salary.pay_period ?? '')) errors.push('Choose the salary pay period.')
      if (!['base', 'total'].includes(salary.basis ?? '')) errors.push('Choose whether salary is base pay or total compensation.')
    }
  }
  value.work_rights?.forEach((right, index) => {
    if (!right.country?.trim()) errors.push(`Work rights entry ${index + 1} requires a country.`)
    if (right.visa_expiry_date && !validDate(right.visa_expiry_date)) errors.push(`Visa expiry date in work rights entry ${index + 1} must use a valid YYYY-MM-DD date.`)
  })
  value.custom_answers?.forEach((answer, index) => {
    if (!answer.question?.trim() || !answer.answer?.trim()) errors.push(`Custom answer ${index + 1} requires both a question and an answer.`)
  })
  return errors
}

// Candidate-entered facts survive profile extraction and YAML import. Extracted
// facts can supply new keys; an existing candidate answer remains authoritative.
export function mergeApplicationDefaults(existing?: ApplicationDefaults, extracted?: ApplicationDefaults): ApplicationDefaults | undefined {
  if (!existing && !extracted) return undefined
  return {
    ...extracted, ...existing,
    ...((existing?.form_answers || extracted?.form_answers) && { form_answers: { ...extracted?.form_answers, ...existing?.form_answers } }),
  }
}
