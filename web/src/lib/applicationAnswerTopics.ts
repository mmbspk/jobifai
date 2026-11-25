import type { ApplicationDefaults } from '../types'

export const applicationAnswerTopicLabels: Record<string, string> = {
  work_rights: 'Right to work', sponsorship: 'Sponsorship', notice_period: 'Notice period',
  earliest_start_date: 'Earliest start', salary: 'Expected pay', relocation: 'Relocation',
  travel: 'Travel', work_arrangement: 'Work arrangement and commute', working_hours: 'Working hours',
  credentials: 'Relevant credentials', additional_answers: 'Additional answers',
}

export function applicationTopicValues(value: ApplicationDefaults): Record<string, unknown> {
  return {
    work_rights: value.work_rights ?? [], sponsorship: { requires_sponsorship: value.requires_sponsorship ?? null, future_requires_sponsorship: value.future_requires_sponsorship ?? null },
    notice_period: { notice_period: value.notice_period ?? '', availability: value.availability ?? '' },
    earliest_start_date: value.earliest_start_date ?? '',
    salary: { salary_expectation: value.salary_expectation ?? '', salary_range: value.salary_range ?? null },
    relocation: value.relocation ?? null, travel: value.travel_conditions ?? '', working_hours: value.working_hours ?? '',
    work_arrangement: { commute_conditions: value.commute_conditions ?? '' },
    additional_answers: { custom_answers: value.custom_answers ?? [], additional_details: value.additional_details ?? '' },
  }
}
