import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Generate } from './Generate'
import { settingsApi } from '../api/settings'
import { botApi, type ApplyTask } from '../api/bot'

vi.mock('../contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'test-user' } }) }))
vi.mock('../api/bot', () => ({ botApi: {
  currentApplyTask: vi.fn(async () => ({ task: null })),
  startApplyTask: vi.fn(), cancelApplyTask: vi.fn(async () => undefined),
} }))

vi.mock('../hooks/useQuota', () => ({ useQuota: () => ({ aiDisabled: false, data: null }) }))
vi.mock('../api/settings', () => ({ settingsApi: {
  markets: { list: vi.fn(async () => []) }, general: { get: vi.fn(async () => ({})) },
  resume: { get: vi.fn(async () => ({})) },
} }))
vi.mock('../state/generationStore', () => ({
  generationStore: { setForm: vi.fn() }, STEPS: {},
  useGenerationState: () => ({
    form: { jobUrl: '', jobDesc: '', market: '', promptHint: '', linkedinUrl: '', githubUrl: '', resumeFile: null, questions: [] },
    inFlight: {}, errors: {}, urlAlerts: {},
    outputs: { resume: { pdfUrl: 'blob:saved-resume' }, cover: { pdfUrl: 'blob:saved-cover' } },
  }),
}))

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  vi.mocked(botApi.currentApplyTask).mockResolvedValue({ task: null })
  vi.mocked(settingsApi.resume.get).mockResolvedValue({})
})

describe('AI Apply retries and background work', () => {
  it.each(['https://au.seek.com/job/95969588', 'https://www.linkedin.com/jobs/view/4494759546/'])('opens the correct AI Apply URL for %s', async url => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={['/generate?' + new URLSearchParams({ tab: 'apply', url })]}>
      <Generate />
    </MemoryRouter></QueryClientProvider>)
    expect(screen.getByPlaceholderText('https://linkedin.com/jobs/view/… or https://seek.com.au/job/…')).toHaveValue(url)
    expect(screen.getAllByRole('button', { name: /^AI Apply$/ })).toHaveLength(2)
    expect(botApi.startApplyTask).not.toHaveBeenCalled()
  })

  it('restores running work and its completed result after leaving the page', async () => {
    const user = userEvent.setup()
    const url = 'https://au.seek.com/job/95969588'
    let serverTask: ApplyTask | null = null
    vi.mocked(botApi.currentApplyTask).mockImplementation(async () => ({ task: serverTask }))
    vi.mocked(botApi.startApplyTask).mockImplementation(async (id, jobUrl, market, force) => {
      serverTask = { id, url: jobUrl, market, force: force ?? false, state: 'running', started_at: new Date().toISOString() }
      return serverTask
    })
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={['/generate?tab=apply&url=' + encodeURIComponent(url)]}>
      <Link to="/elsewhere">Leave Generate</Link>
      <Routes>
        <Route path="/generate" element={<Generate />} />
        <Route path="/elsewhere" element={<Link to="/generate?tab=apply">Return to Generate</Link>} />
      </Routes>
    </MemoryRouter></QueryClientProvider>)
    const apply = screen.getAllByRole('button', { name: /^AI Apply$/ })[1]
    await waitFor(() => expect(apply).toBeEnabled())
    await user.click(apply)
    await screen.findByText('AI Apply is running')
    await user.click(screen.getByText('Leave Generate'))
    expect(screen.queryByText('AI Apply is running')).not.toBeInTheDocument()
    await user.click(screen.getByText('Return to Generate'))
    await screen.findByText('AI Apply is running')
    expect(botApi.startApplyTask).toHaveBeenCalledTimes(1)
    await user.click(screen.getByText('Leave Generate'))
    serverTask = { ...serverTask!, state: 'succeeded', response: { status: 'score_warning', score: 5, company: 'Company', role: 'Engineer', reasoning: 'Role requires more relevant experience.' } }
    // A full refresh discards client query state; the server still holds the result.
    qc.clear()
    await user.click(screen.getByText('Return to Generate'))
    await screen.findByText('Role requires more relevant experience.')
    expect(screen.queryByText('AI Apply is running')).not.toBeInTheDocument()
    expect(botApi.startApplyTask).toHaveBeenCalledTimes(1)
  })
})

function renderGenerate(tab = 'evaluate') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[`/generate?tab=${tab}`]}>
    <Generate />
  </MemoryRouter></QueryClientProvider>)
  return qc
}

describe('Generate saved document result', () => {
  it.each(['resume', 'cover'])('links the %s result to Documents and refreshes cached documents', async tab => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData(['documents'], { documents: [] })
    render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[`/generate?tab=${tab}`]}>
      <Generate />
    </MemoryRouter></QueryClientProvider>)
    expect(screen.getByRole('link', { name: 'Saved to Documents' })).toHaveAttribute('href', '/documents')
    await waitFor(() => expect(qc.getQueryState(['documents'])?.isInvalidated).toBe(true))
  })
})

describe('Generate profile link shortcuts', () => {
  it('copies each saved profile URL and shows the copy hint on hover', async () => {
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText')
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: {
      linkedin: ' https://linkedin.com/in/candidate ', github: 'https://github.com/candidate',
    } })
    renderGenerate()
    const linkedin = await screen.findByRole('button', { name: 'Copy LinkedIn profile link' })
    await user.hover(linkedin)
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Click to copy the link')
    await user.click(linkedin)
    expect(writeText).toHaveBeenLastCalledWith('https://linkedin.com/in/candidate')
    expect(screen.getByRole('status')).toHaveTextContent('LinkedIn link copied')
    await user.click(screen.getByRole('button', { name: 'Copy GitHub profile link' }))
    expect(writeText).toHaveBeenLastCalledWith('https://github.com/candidate')
    expect(screen.getByRole('status')).toHaveTextContent('GitHub link copied')
  })

  it('greys out missing links and keeps their setup hints available to hover and keyboard focus', async () => {
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText')
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: { linkedin: '   ' } })
    renderGenerate()
    await waitFor(() => expect(settingsApi.resume.get).toHaveBeenCalled())
    const linkedin = screen.getByRole('button', { name: 'LinkedIn profile link unavailable' })
    const github = screen.getByRole('button', { name: 'GitHub profile link unavailable' })
    expect(linkedin).toHaveAttribute('aria-disabled', 'true')
    expect(github).toHaveAttribute('aria-disabled', 'true')
    expect(linkedin).toHaveClass('opacity-40')
    await user.hover(linkedin)
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Set up your LinkedIn link in Profile to make it available here')
    await user.click(linkedin)
    await user.unhover(linkedin)
    await user.tab()
    expect(github).toHaveFocus()
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Set up your GitHub link in Profile to make it available here')
    await user.keyboard('{Enter}')
    expect(writeText).not.toHaveBeenCalled()
  })

  it('reports clipboard failure without claiming that the link was copied', async () => {
    const user = userEvent.setup()
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('Clipboard denied'))
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: { github: 'https://github.com/candidate' } })
    renderGenerate('resume')
    await user.click(await screen.findByRole('button', { name: 'Copy GitHub profile link' }))
    expect(screen.getByRole('status')).toHaveTextContent('Could not copy your GitHub link. Please try again.')
  })

  it('keeps both shortcuts available when switching Generate tabs', async () => {
    const user = userEvent.setup()
    vi.mocked(settingsApi.resume.get).mockResolvedValue({ personal_information: {
      linkedin: 'https://linkedin.com/in/candidate', github: 'https://github.com/candidate',
    } })
    renderGenerate()
    await screen.findByRole('button', { name: 'Copy LinkedIn profile link' })
    for (const tab of ['Resume', 'Cover Letter', 'Questions', 'AI Apply', 'Job Fit']) {
      await user.click(screen.getByRole('button', { name: tab }))
      expect(screen.getByRole('button', { name: 'Copy LinkedIn profile link' })).toHaveAttribute('aria-disabled', 'false')
      expect(screen.getByRole('button', { name: 'Copy GitHub profile link' })).toHaveAttribute('aria-disabled', 'false')
    }
  })
})
