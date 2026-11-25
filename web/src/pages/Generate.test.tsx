import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Generate } from './Generate'
import { settingsApi } from '../api/settings'

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
  vi.mocked(settingsApi.resume.get).mockResolvedValue({})
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
