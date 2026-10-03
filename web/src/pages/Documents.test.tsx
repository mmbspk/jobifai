import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { Documents } from './Documents'

vi.mock('../hooks/useQuota', () => ({
  useQuota: () => ({ aiDisabled: false, data: null }),
}))

vi.mock('../api/documents', () => ({
  documentsApi: {
    list: vi.fn(async () => ({ documents: [], defaults: {} })),
    getVersion: vi.fn(),
    createResumeFromProfile: vi.fn(),
    appendResumeVersion: vi.fn(),
    saveCoverLetter: vi.fn(),
    appendCoverVersion: vi.fn(),
    aiImproveResume: vi.fn(),
    aiGenerateCover: vi.fn(),
    setDefault: vi.fn(),
  },
  fetchDocumentPdf: vi.fn(),
  fetchDocumentOriginal: vi.fn(),
}))

vi.mock('../api/settings', () => ({
  settingsApi: {
    general: { get: vi.fn(async () => ({ default_resume_market: '' })) },
    styles: { list: vi.fn(async () => []) },
  },
}))

describe('Documents page', () => {
  it('renders empty state when documents list is empty', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <Documents />
      </QueryClientProvider>,
    )

    expect(await screen.findByText(/No saved documents yet/i)).toBeInTheDocument()
  })
})
