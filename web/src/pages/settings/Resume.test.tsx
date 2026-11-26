import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { settingsApi } from '../../api/settings'
import { Resume } from './Resume'
import type { ResumeProfile } from '../../types'

vi.mock('../../api/settings', () => ({ settingsApi: {
  resume: { get: vi.fn(), set: vi.fn(), upload: vi.fn(), downloadUrl: () => '/export.yaml' },
  applicationAnswers: { get: vi.fn(), set: vi.fn(), reviews: vi.fn(), markRead: vi.fn(), confirm: vi.fn() },
} }))
const profile: ResumeProfile = {
  personal_information: { name: 'Candidate', email: 'candidate@example.com', city: 'Adelaide' },
  skills: ['Client service'], prompt_instructions: 'Use concise examples',
  application_defaults: { notice_period: '3 weeks', requires_sponsorship: false, form_answers: { reviewed: 'Existing answer' }, custom_answers: [{ question: 'Can you travel?', answer: 'Occasionally' }] },
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.resume.get).mockResolvedValue(profile)
  vi.mocked(settingsApi.resume.set).mockResolvedValue(undefined)
  vi.mocked(settingsApi.applicationAnswers.get).mockResolvedValue({ application_defaults: profile.application_defaults!, revision: 'r1', sources: {}, catalog: [] })
  vi.mocked(settingsApi.applicationAnswers.reviews).mockResolvedValue({ items: [], unresolved_count: 0, unread_count: 0 })
})
function renderProfile() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const rendered = render(<MemoryRouter><QueryClientProvider client={client}><Resume /></QueryClientProvider></MemoryRouter>)
  return { ...rendered, client }
}

describe('Profile drafts', () => {
  it('waits for saved profile hydration before exposing editable fields', async () => {
    let finish!: (value: ResumeProfile) => void
    vi.mocked(settingsApi.resume.get).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    renderProfile()
    expect(screen.getByRole('status')).toHaveTextContent('Loading your profile')
    expect(screen.queryByLabelText('Postal code')).not.toBeInTheDocument()
    await act(async () => finish({ ...profile, personal_information: { ...profile.personal_information, zip_code: '7000' } }))
    expect(await screen.findByLabelText('Postal code')).toHaveValue('7000')
  })
  it('reviews imported common answers and saves missing facts with the first profile', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.get).mockRejectedValue(new Error('No saved profile'))
    vi.mocked(settingsApi.applicationAnswers.get).mockResolvedValue({ application_defaults: {}, revision: 'empty', sources: {}, catalog: [], profile_revision: 'empty-profile' })
    vi.mocked(settingsApi.resume.upload).mockResolvedValue({ personal_information: { name: 'Imported candidate' }, application_defaults: { notice_period: '2 weeks', requires_sponsorship: false }, profile_revision: 'empty-profile', import_id: 'owned-cv-import', application_answer_prefill: [{ topic_id: 'notice_period', binding: 'application_defaults.notice_period', value: '2 weeks', source: 'cv_evidence', review_state: 'suggested', evidence: [{ quote: 'Notice period: 2 weeks.', start: 0, end: 23 }] }] })
    const { container } = renderProfile()
    await screen.findByLabelText('Postal code')
    await user.upload(container.querySelector('input[type="file"]') as HTMLInputElement, new File(['CV'], 'candidate.pdf', { type: 'application/pdf' }))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await screen.findByText(/Profile extracted, review/)
    await user.click(screen.getByRole('tab', { name: 'Application answers' }))
    await user.click(screen.getByRole('button', { name: /Notice period.*2 weeks/ }))
    expect(screen.getByLabelText('Notice period', { exact: true })).toHaveValue('2 weeks')
    await user.type(screen.getByLabelText('Availability details'), 'Weekdays')
    expect(settingsApi.resume.set).not.toHaveBeenCalled()
    expect(settingsApi.applicationAnswers.set).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save profile and answers' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith(expect.objectContaining({ personal_information: { name: 'Imported candidate' }, application_defaults: { notice_period: '2 weeks', requires_sponsorship: false, availability: 'Weekdays' }, profile_revision: 'empty-profile', import_id: 'owned-cv-import' })))
    expect(settingsApi.applicationAnswers.set).not.toHaveBeenCalled()
  })
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
  it('keeps unsaved CV edits mounted through tab changes and profile invalidation', async () => {
    const user = userEvent.setup()
    const { client } = renderProfile()
    await screen.findByLabelText('Phone calling code')
    await user.type(screen.getByLabelText('Postal code'), '7000')
    await user.click(screen.getByRole('tab', { name: 'Application answers' }))
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ ...profile, personal_information: { ...profile.personal_information, zip_code: '9999' } })
    await act(async () => { await client.invalidateQueries({ queryKey: ['settings-resume'] }) })
    await user.click(screen.getByRole('tab', { name: 'CV & profile' }))
    expect(screen.getByLabelText('Postal code')).toHaveValue('7000')
  })
  it('preserves saved facts and newer CV edits while extraction is pending', async () => {
    const user = userEvent.setup()
    let finish!: (profile: ResumeProfile) => void
    vi.mocked(settingsApi.resume.upload).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const { container } = renderProfile()
    await screen.findByLabelText('Postal code')
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, new File(['profile'], 'profile.yaml', { type: 'application/yaml' }))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await user.type(screen.getByLabelText('Postal code'), '7000')
    await act(async () => finish({ personal_information: { name: 'Imported', zip_code: '9999' }, summary: 'Imported summary', application_defaults: { notice_period: 'Wrong extracted notice', availability: 'Weekdays' } }))
    await screen.findByText(/Profile extracted, review/)
    expect(screen.getByLabelText('Postal code')).toHaveValue('7000')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith(expect.objectContaining({ personal_information: { ...profile.personal_information, name: 'Imported', zip_code: '7000' }, summary: 'Imported summary', application_defaults: { ...profile.application_defaults, availability: 'Weekdays' } })))
  })
  it('rebases separately saved facts without discarding reviewed import additions or unsaved CV edits', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.upload).mockResolvedValue({ summary: 'Imported summary', application_defaults: { availability: 'Weekdays' }, import_id: 'owned-import', application_answer_prefill: [], profile_revision: 'before-import' })
    const { container, client } = renderProfile()
    await screen.findByLabelText('Postal code')
    await user.type(screen.getByLabelText('Postal code'), '7000')
    await user.upload(container.querySelector('input[type="file"]') as HTMLInputElement, new File(['profile'], 'profile.yaml'))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await screen.findByText(/Profile extracted, review/)
    await act(async () => { client.setQueryData(['application-answers'], { application_defaults: { ...profile.application_defaults, notice_period: '6 weeks' }, revision: 'r2', profile_revision: 'after-answer-save', sources: {}, catalog: [], profile }) })
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Imported summary', personal_information: { ...profile.personal_information, zip_code: '7000' }, application_defaults: { ...profile.application_defaults, notice_period: '6 weeks', availability: 'Weekdays' }, profile_revision: 'after-answer-save', import_id: 'owned-import' })))
  })
  it('requires a decision when reviewed import facts and independently saved facts compete', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.upload).mockResolvedValue({ application_defaults: { availability: 'Weekdays' } })
    const { container, client } = renderProfile()
    await screen.findByLabelText('Postal code')
    await user.upload(container.querySelector('input[type="file"]') as HTMLInputElement, new File(['profile'], 'profile.yaml'))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await screen.findByText(/Profile extracted, review/)
    await act(async () => { client.setQueryData(['application-answers'], { application_defaults: { ...profile.application_defaults, availability: 'Weekends' }, revision: 'r2', sources: {}, catalog: [] }) })
    await screen.findByText(/Saved application answers also changed: availability/)
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Keep my reviewed CV answers' }))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith(expect.objectContaining({ application_defaults: { ...profile.application_defaults, availability: 'Weekdays' } })))
  })
  it('uses the upload revision for a first profile when no saved profile exists', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.get).mockRejectedValue(Object.assign(new Error('No saved profile'), { status: 404 }))
    vi.mocked(settingsApi.applicationAnswers.get).mockResolvedValue({ application_defaults: {}, revision: 'empty-answers', sources: {}, catalog: [] })
    vi.mocked(settingsApi.resume.upload).mockResolvedValue({ personal_information: { name: 'First candidate' }, profile_revision: 'empty-profile' })
    const { container } = renderProfile()
    await screen.findByLabelText('Postal code')
    await user.upload(container.querySelector('input[type="file"]') as HTMLInputElement, new File(['profile'], 'profile.yaml'))
    await user.click(screen.getByRole('button', { name: 'Extract with AI' }))
    await screen.findByText(/Profile extracted, review/)
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(settingsApi.resume.set).toHaveBeenCalledWith(expect.objectContaining({ personal_information: { name: 'First candidate' }, profile_revision: 'empty-profile' })))
  })
  it('keeps two empty onboarding sessions versioned so the second first-save cannot overwrite the first', async () => {
    const user = userEvent.setup()
    let savedProfile: ResumeProfile | undefined
    let revision = 'empty-profile'
    vi.mocked(settingsApi.resume.get).mockImplementation(async () => {
      if (!savedProfile) throw Object.assign(new Error('No saved profile'), { status: 404 })
      return { ...savedProfile, profile_revision: revision }
    })
    vi.mocked(settingsApi.applicationAnswers.get).mockImplementation(async () => ({ application_defaults: savedProfile?.application_defaults ?? {}, revision: savedProfile ? 'saved-answers' : 'empty-answers', profile_revision: revision, sources: {}, catalog: [] }))
    vi.mocked(settingsApi.resume.set).mockImplementation(async body => {
      if (body.profile_revision !== revision) throw Object.assign(new Error('Profile changed; reload before saving'), { status: 409 })
      const { profile_revision: _version, ...value } = body
      void _version
      savedProfile = value
      revision = 'saved-profile'
    })
    const first = renderProfile()
    const second = renderProfile()
    const firstView = within(first.container), secondView = within(second.container)
    await waitFor(() => expect(firstView.getByRole('button', { name: 'Save changes' })).toBeEnabled())
    await waitFor(() => expect(secondView.getByRole('button', { name: 'Save changes' })).toBeEnabled())
    await user.type(firstView.getByLabelText('Postal code'), '1000')
    await user.type(secondView.getByLabelText('Postal code'), '2000')
    await user.click(firstView.getByRole('button', { name: 'Save changes' }))
    await firstView.findByRole('button', { name: 'Saved' })
    // Returning to the second tab can refetch the profile that was just saved.
    // Its first successful GET must not replace the already edited empty draft.
    await act(async () => { await second.client.invalidateQueries({ queryKey: ['settings-resume'] }) })
    expect(secondView.getByLabelText('Postal code')).toHaveValue('2000')
    await user.click(secondView.getByRole('button', { name: 'Save changes' }))
    await secondView.findByText(/Profile changed; reload before saving/)
    expect(settingsApi.resume.set).toHaveBeenNthCalledWith(1, expect.objectContaining({ profile_revision: 'empty-profile', personal_information: { zip_code: '1000' } }))
    expect(settingsApi.resume.set).toHaveBeenNthCalledWith(2, expect.objectContaining({ profile_revision: 'empty-profile', personal_information: { zip_code: '2000' } }))
    expect(savedProfile?.personal_information?.zip_code).toBe('1000')
    expect(secondView.getByLabelText('Postal code')).toHaveValue('2000')
  })
})
