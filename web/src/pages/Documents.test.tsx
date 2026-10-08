import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { Documents } from './Documents'
import { settingsApi } from '../api/settings'
import { documentsApi } from '../api/documents'
import type { ResumeStyle } from '../types'

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
    setPreferredStyle: vi.fn(async () => undefined),
  },
  fetchDocumentPdf: vi.fn(),
  fetchDocumentOriginal: vi.fn(),
}))

vi.mock('../api/settings', () => ({
  settingsApi: {
    general: { get: vi.fn(async () => ({ default_resume_market: '' })) },
    styles: { list: vi.fn(async (): Promise<ResumeStyle[]> => []) },
  },
}))

describe('Documents page', () => {
  it('shows friendly style labels while sending the existing style value', async () => {
    vi.mocked(settingsApi.styles.list).mockResolvedValueOnce([
      { name: 'Au', display_name: 'Australia', css_file: 'style_au.css' },
      { name: 'My Design', css_file: 'style_my_design.css' },
    ])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}><Documents /></QueryClientProvider>)
    expect(await screen.findByRole('option', { name: 'Australia' })).toHaveValue('Au')
    expect(screen.getByRole('option', { name: 'My Design' })).toHaveValue('My Design')
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'Au' } })
    fireEvent.click(screen.getByRole('button', { name: 'Resume from profile (no AI)' }))
    await waitFor(() => expect(documentsApi.createResumeFromProfile).toHaveBeenCalledWith(
      expect.objectContaining({ style: 'Au' }),
    ))
  })

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
