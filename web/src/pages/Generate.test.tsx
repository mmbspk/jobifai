import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { Generate } from './Generate'

vi.mock('../hooks/useQuota', () => ({ useQuota: () => ({ aiDisabled: false, data: null }) }))
vi.mock('../api/settings', () => ({ settingsApi: {
  markets: { list: vi.fn(async () => []) }, general: { get: vi.fn(async () => ({})) },
} }))
vi.mock('../state/generationStore', () => ({
  generationStore: { setForm: vi.fn() }, STEPS: {},
  useGenerationState: () => ({
    form: { jobUrl: '', jobDesc: '', market: '', promptHint: '', linkedinUrl: '', githubUrl: '', resumeFile: null, questions: [] },
    inFlight: {}, errors: {}, urlAlerts: {},
    outputs: { resume: { pdfUrl: 'blob:saved-resume' }, cover: { pdfUrl: 'blob:saved-cover' } },
  }),
}))

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
