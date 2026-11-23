import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { settingsApi } from '../../api/settings'
import { Resume } from './Resume'
import type { ResumeProfile } from '../../types'

vi.mock('../../api/settings', () => ({ settingsApi: { resume: { get: vi.fn(), set: vi.fn(), upload: vi.fn(), downloadUrl: () => '/export.yaml' } } }))
vi.mock('../../components/settings/FormMappingReview', () => ({ FormMappingReview: () => null }))

const profile: ResumeProfile = {
  personal_information: { name: 'Candidate', email: 'candidate@example.com', city: 'Adelaide' },
  skills: ['Client service'], prompt_instructions: 'Use concise examples',
  application_defaults: { notice_period: '3 weeks', requires_sponsorship: false, form_answers: { reviewed: 'Existing answer' }, custom_answers: [{ question: 'Can you travel?', answer: 'Occasionally' }] },
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.resume.get).mockResolvedValue(profile)
  vi.mocked(settingsApi.resume.set).mockResolvedValue(undefined)
})
function renderProfile() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><Resume /></QueryClientProvider>)
}

describe('Profile application answers', () => {
  it('saves calling code and postal information while preserving application answers', async () => {
    const user = userEvent.setup()
    renderProfile()
    await screen.findByLabelText('Phone calling code')
    await user.type(screen.getByLabelText('Phone calling code'), '+61')
    await user.type(screen.getByLabelText('Postal code'), '7000')
    await user.type(screen.getByLabelText('Street address'), 'Example street')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith({ ...profile, personal_information: { ...profile.personal_information, phone_prefix: '+61', zip_code: '7000', address: 'Example street' } }))
  })
  it('saves edited application facts with the rest of the existing profile', async () => {
    const user = userEvent.setup()
    renderProfile()
    await user.click(screen.getByRole('button', { name: 'Application answers' }))
    await waitFor(() => expect(screen.getByLabelText('Notice period')).toHaveValue('3 weeks'))
    await user.clear(screen.getByLabelText('Notice period'))
    await user.type(screen.getByLabelText('Notice period'), 'Immediately available')
    await user.type(screen.getByLabelText('Additional application details'), 'Available for occasional travel')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith({ ...profile, application_defaults: { ...profile.application_defaults, notice_period: 'Immediately available', additional_details: 'Available for occasional travel' } }))
  })

  it('preserves saved answers and unrelated profile fields through extraction and saving', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.upload).mockResolvedValue({ personal_information: { name: 'Imported' }, summary: 'Imported summary', application_defaults: { notice_period: 'Wrong extracted notice', availability: 'Weekdays' } })
    const { container } = renderProfile()
    await user.click(screen.getByRole('button', { name: 'Application answers' }))
    await waitFor(() => expect(screen.getByLabelText('Notice period')).toHaveValue('3 weeks'))
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, new File(['profile'], 'profile.yaml', { type: 'application/yaml' }))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await screen.findByText(/Profile extracted, review/)
    expect(screen.getByLabelText('Notice period')).toHaveValue('3 weeks')
    expect(screen.getByLabelText('Availability details')).toHaveValue('Weekdays')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith({
      ...profile, personal_information: { ...profile.personal_information, name: 'Imported' }, summary: 'Imported summary',
      application_defaults: { ...profile.application_defaults, availability: 'Weekdays' },
    }))
  })

  it('blocks saving an unfinished custom answer and retains it for editing', async () => {
    const user = userEvent.setup()
    renderProfile()
    await user.click(screen.getByRole('button', { name: 'Application answers' }))
    await waitFor(() => expect(screen.getByLabelText('Notice period')).toHaveValue('3 weeks'))
    await user.click(screen.getByRole('button', { name: 'Add custom question' }))
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
    expect(screen.getAllByRole('alert').some(alert => alert.textContent?.includes('both a question and an answer'))).toBe(true)
    expect(settingsApi.resume.set).not.toHaveBeenCalled()
  })
})
