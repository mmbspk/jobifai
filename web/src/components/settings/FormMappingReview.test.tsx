import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { settingsApi } from '../../api/settings'
import { FormMappingReview } from './FormMappingReview'
import type { ResumeProfile } from '../../types'

vi.mock('../../api/settings', () => ({ settingsApi: { formMappings: { list: vi.fn(), confirm: vi.fn(), invalidate: vi.fn() }, resume: { get: vi.fn() } } }))
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.formMappings.list).mockResolvedValue({ entries: [{
    id: 'mapping-1', platform: 'seek', employer_key: 'Acme',
    field: { label: 'How soon can you leave?', type: 'text', group: 'Availability' },
    binding_key: 'application_defaults.notice_period', provenance: 'pending',
  }] })
  vi.mocked(settingsApi.formMappings.confirm).mockResolvedValue(undefined)
  vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: { name: 'Jane' }, application_defaults: { notice_period: 'Three weeks' } } as ResumeProfile)
})
function renderReview() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><FormMappingReview /></QueryClientProvider>)
}
describe('FormMappingReview', () => {
  it('shows automatically verified mappings without requiring candidate confirmation', async () => {
    vi.mocked(settingsApi.formMappings.list).mockResolvedValue({ entries: [{
      id: 'automatic', platform: 'seek', employer_key: 'Acme',
      field: { label: 'Where are you based?', type: 'radio', job_title: 'Coordinator', job_location: 'Adelaide' },
      binding_key: '', provenance: 'verified_by_model',
    }] })
    vi.mocked(settingsApi.formMappings.invalidate).mockResolvedValue(undefined)
    renderReview()
    await screen.findByText(/Verified automatically/)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Confirm question mapping' })).not.toBeInTheDocument()
    expect(settingsApi.formMappings.confirm).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Disable mapping' }))
    await waitFor(() => expect(settingsApi.formMappings.invalidate).toHaveBeenCalledWith('automatic'))
    await userEvent.click(screen.getByRole('button', { name: 'Review or override' }))
    expect(screen.getByRole('button', { name: 'Confirm question mapping' })).toBeDisabled()
  })
  it('shows missing sponsorship separately from an explicit no', async () => {
    vi.mocked(settingsApi.formMappings.list).mockResolvedValue({ entries: [{ id: 'sponsor', platform: 'seek', employer_key: 'Acme', field: { label: 'Require sponsorship?', type: 'radio', options: ['Yes', 'No'] }, binding_key: 'application_defaults.requires_sponsorship', provenance: 'pending' }] })
    renderReview()
    await screen.findByText(/Current saved answer: Missing/)
    await userEvent.click(screen.getByRole('checkbox'))
    expect(screen.getByRole('button', { name: 'Confirm question mapping' })).toBeDisabled()
  })
  it('requires explicit confirmation before promoting a proposed binding', async () => {
    renderReview()
    const button = await screen.findByRole('button', { name: 'Confirm question mapping' })
    expect(button).toBeDisabled()
    expect(settingsApi.formMappings.confirm).not.toHaveBeenCalled()
    expect(await screen.findByText('Current saved answer: Three weeks')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.click(button)
    await waitFor(() => expect(settingsApi.formMappings.confirm).toHaveBeenCalledWith('mapping-1', {
      confirmed: true, binding_key: 'application_defaults.notice_period', answer: undefined,
    }))
  })
  it('lets the candidate supply an explicit missing answer', async () => {
    renderReview()
    const select = await screen.findByLabelText('Answer source for How soon can you leave?')
    await userEvent.selectOptions(select, '')
    await userEvent.type(screen.getByLabelText('Answer for How soon can you leave?'), 'Three weeks')
    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Confirm question mapping' }))
    await waitFor(() => expect(settingsApi.formMappings.confirm).toHaveBeenCalledWith('mapping-1', {
      confirmed: true, binding_key: undefined, answer: 'Three weeks',
    }))
  })
  it('resets confirmation when the answer source changes', async () => {
    renderReview()
    await screen.findByText('How soon can you leave?')
    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.selectOptions(screen.getByLabelText('Answer source for How soon can you leave?'), 'personal_information.city')
    expect(screen.getByRole('button', { name: 'Confirm question mapping' })).toBeDisabled()
  })
  it('keeps a mapping with a missing saved answer unconfirmed', async () => {
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: { name: 'Jane' }, application_defaults: { notice_period: '' } } as ResumeProfile)
    renderReview()
    await screen.findByText(/Missing — update your profile/)
    await userEvent.click(screen.getByRole('checkbox'))
    expect(screen.getByRole('button', { name: 'Confirm question mapping' })).toBeDisabled()
    expect(settingsApi.formMappings.confirm).not.toHaveBeenCalled()
  })
})
