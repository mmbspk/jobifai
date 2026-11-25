import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { settingsApi, type ApplicationAnswersResponse } from '../../api/settings'
import { ApplicationAnswers } from './ApplicationAnswers'

vi.mock('../../api/settings', () => ({ settingsApi: { applicationAnswers: { get: vi.fn(), set: vi.fn(), reviews: vi.fn(), markRead: vi.fn(), confirm: vi.fn(), dismiss: vi.fn(), review: vi.fn() } } }))
const saved: ApplicationAnswersResponse = { application_defaults: { notice_period: '3 weeks', requires_sponsorship: false, form_answers: { old: 'Keep this' }, custom_answers: [{ question: 'Travel?', answer: 'Occasionally', country: 'Australia' }] }, revision: 'r1', sources: { notice_period: { source: 'cv' } }, catalog: [] }
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.applicationAnswers.get).mockResolvedValue(saved)
  vi.mocked(settingsApi.applicationAnswers.set).mockResolvedValue({ ...saved, revision: 'r2' })
  vi.mocked(settingsApi.applicationAnswers.reviews).mockResolvedValue({ items: [], unresolved_count: 0, unread_count: 0 })
  vi.mocked(settingsApi.applicationAnswers.markRead).mockResolvedValue(undefined)
})
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(<MemoryRouter><QueryClientProvider client={client}><ApplicationAnswers onEditProfile={vi.fn()} /></QueryClientProvider></MemoryRouter>)
  return client
}
async function openNotice(user: ReturnType<typeof userEvent.setup>) { await screen.findByRole('button', { name: /Notice period.*3 weeks/ }); await user.click(screen.getByRole('button', { name: /Notice period.*3 weeks/ })) }

describe('Application answers', () => {
  it('shows five compact core rows and preserves false versus unspecified sponsorship', async () => {
    const user = userEvent.setup()
    mount()
    const core = await screen.findByRole('region', { name: 'Common answers' })
    expect(within(core).getAllByRole('button')).toHaveLength(5)
    expect(screen.queryByLabelText('Salary minimum')).not.toBeVisible()
    expect(screen.queryByRole('button', { name: /Travel Not specified/ })).toBeNull()
    await user.click(screen.getByRole('button', { name: /Sponsorship.*Not required/ }))
    expect(screen.getByLabelText('General sponsorship requirement')).toHaveValue('no')
    await user.selectOptions(screen.getByLabelText('General sponsorship requirement'), '')
    await user.click(screen.getByRole('button', { name: 'Save common answers' }))
    await waitFor(() => expect(settingsApi.applicationAnswers.set).toHaveBeenCalledWith({ revision: 'r1', topics: { sponsorship: { requires_sponsorship: null, future_requires_sponsorship: null } } }))
  })
  it('retains dirty answers across tabs, remote updates and failed saves', async () => {
    const user = userEvent.setup()
    const client = mount()
    await openNotice(user)
    await user.clear(screen.getByLabelText('Notice period'))
    await user.type(screen.getByLabelText('Notice period'), '4 weeks')
    await user.click(screen.getByRole('tab', { name: /Needs confirmation/ }))
    await user.click(screen.getByRole('tab', { name: 'Common answers' }))
    expect(screen.getByLabelText('Notice period')).toHaveValue('4 weeks')
    vi.mocked(settingsApi.applicationAnswers.get).mockResolvedValue({ ...saved, revision: 'remote-r2', application_defaults: { ...saved.application_defaults, notice_period: '6 weeks' } })
    await act(async () => { await client.invalidateQueries({ queryKey: ['application-answers'] }) })
    expect(screen.getByLabelText('Notice period')).toHaveValue('4 weeks')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save common answers' })).toBeDisabled())
    await user.click(screen.getByRole('button', { name: 'Load latest answers and keep my edits' }))
    vi.mocked(settingsApi.applicationAnswers.set).mockRejectedValue(new Error('Save failed'))
    await user.click(screen.getByRole('button', { name: 'Save common answers' }))
    await screen.findByText(/Save failed/)
    expect(screen.getByLabelText('Notice period')).toHaveValue('4 weeks')
    expect(settingsApi.applicationAnswers.set).toHaveBeenLastCalledWith({ revision: 'remote-r2', topics: { notice_period: { notice_period: '4 weeks', availability: '' } } })
  })
  it('keeps a newer edit made while a save is pending', async () => {
    const user = userEvent.setup()
    let finish!: (response: ApplicationAnswersResponse) => void
    vi.mocked(settingsApi.applicationAnswers.set).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    mount()
    await openNotice(user)
    await user.clear(screen.getByLabelText('Notice period'))
    await user.type(screen.getByLabelText('Notice period'), '4 weeks')
    await user.click(screen.getByRole('button', { name: 'Save common answers' }))
    await user.clear(screen.getByLabelText('Notice period'))
    await user.type(screen.getByLabelText('Notice period'), '5 weeks')
    await act(async () => finish({ ...saved, revision: 'r2', application_defaults: { ...saved.application_defaults, notice_period: '4 weeks' } }))
    expect(screen.getByLabelText('Notice period')).toHaveValue('5 weeks')
  })
  it('marks reading separately and confirms the exact answer from an older page', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.applicationAnswers.reviews).mockImplementation(async cursor => cursor ? { items: [{ id: 'older/item', question: 'Can you work weekends?', status: 'used', answer_used: 'One weekend per month', revision: 7, evidence: ['Saved availability'] }], unresolved_count: 1, unread_count: 0 } : { items: [], unresolved_count: 1, unread_count: 1, next_cursor: 'page2' })
    vi.mocked(settingsApi.applicationAnswers.confirm).mockResolvedValue({ value: saved.application_defaults, revision: 'r2' })
    mount()
    await screen.findByRole('button', { name: '1 new answer' })
    await user.click(screen.getByRole('tab', { name: /Needs confirmation/ }))
    await waitFor(() => expect(settingsApi.applicationAnswers.markRead).toHaveBeenCalled())
    expect(settingsApi.applicationAnswers.confirm).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Load more answers' }))
    await screen.findByText('Answer actually used')
    await user.click(screen.getByRole('button', { name: 'Confirm for future use' }))
    await waitFor(() => expect(settingsApi.applicationAnswers.confirm).toHaveBeenCalledWith('older/item', { answer: 'One weekend per month', revision: 7 }))
  })
  it('keeps invalid pay input visible when its editor is closed and reopened', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findByRole('button', { name: /Expected pay.*Not specified/ })
    await user.click(screen.getByRole('button', { name: /Expected pay.*Not specified/ }))
    await user.type(screen.getByLabelText('Salary minimum'), 'not a number')
    await user.click(screen.getByRole('button', { name: /Notice period.*3 weeks/ }))
    await user.click(screen.getByRole('button', { name: /Expected pay/ }))
    expect(screen.getByLabelText('Salary minimum')).toHaveValue('not a number')
    expect(screen.getByRole('button', { name: 'Save common answers' })).toBeDisabled()
  })
  it('keeps the version of a review that was being edited when fresh data arrives', async () => {
    const user = userEvent.setup()
    const item = { id: 'review', question: 'Can you travel?', status: 'used' as const, answer_used: 'Occasionally', revision: 1, use_status: 'submission_unknown' as const }
    vi.mocked(settingsApi.applicationAnswers.reviews).mockResolvedValue({ items: [item], unresolved_count: 1, unread_count: 0 })
    vi.mocked(settingsApi.applicationAnswers.confirm).mockRejectedValue(new Error('Review changed; reload'))
    const client = mount()
    await user.click(screen.getByRole('tab', { name: /Needs confirmation/ }))
    await screen.findByText('Answer filled — submission outcome unknown')
    await user.click(screen.getByRole('button', { name: 'Edit for future use' }))
    await user.clear(screen.getByLabelText('Answer for future use'))
    await user.type(screen.getByLabelText('Answer for future use'), 'With two weeks notice')
    vi.mocked(settingsApi.applicationAnswers.reviews).mockResolvedValue({ items: [{ ...item, revision: 2 }], unresolved_count: 1, unread_count: 0 })
    await act(async () => { await client.invalidateQueries({ queryKey: ['application-answer-reviews'] }) })
    await user.click(screen.getByRole('button', { name: 'Save and confirm for future use' }))
    await waitFor(() => expect(settingsApi.applicationAnswers.confirm).toHaveBeenCalledWith('review', { answer: 'With two weeks notice', revision: 1 }))
    await screen.findByText(/Review changed; reload/)
    expect(screen.getByLabelText('Answer for future use')).toHaveValue('With two weeks notice')
  })
})
