import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Documents } from './Documents'
import { settingsApi } from '../api/settings'
import { documentsApi } from '../api/documents'
import type { DocumentsListResponse } from '../api/documents'
import type { ResumeProfile, ResumeStyle } from '../types'

beforeEach(() => vi.clearAllMocks())

vi.mock('../hooks/useQuota', () => ({
  useQuota: () => ({ aiDisabled: false, data: null }),
}))

vi.mock('../api/documents', () => ({
  documentsApi: {
    list: vi.fn(async (): Promise<DocumentsListResponse> => ({ documents: [], defaults: {} })),
    getVersion: vi.fn(),
    deleteDocument: vi.fn(async () => ({ message: 'document deleted' })),
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

const savedProfile: ResumeProfile = {
  summary: 'Original summary', skills: ['Go', 'Operations'],
  personal_information: { name: 'Sam' },
  experience_details: [{ position: 'Lead', company: 'Acme', employment_period: '2020–2024',
    key_responsibilities: ['Delivered the project'], skills_acquired: ['Planning'] }],
  languages: [{ language: 'English', proficiency: 'Fluent' }],
  projects: [{ name: 'Unchanged project' }],
}

function mockSavedDocument(kind: 'resume' | 'cover_letter', isDefault = false) {
  vi.mocked(documentsApi.list).mockResolvedValueOnce({
    documents: [{ id: 'doc1', kind, title: 'Saved document', created_at: '', updated_at: '', versions: [{
      id: 'v1', version_number: 1, source: 'user_edit', content_kind: kind === 'resume' ? 'resume_json' : 'cover_text',
      reconstructible: true, has_pdf: true, created_at: '',
    }] }], defaults: isDefault ? { resume_version_id: 'v1' } : {},
  })
  vi.mocked(documentsApi.getVersion).mockResolvedValueOnce({
    id: 'v1', document_id: 'doc1', content_kind: kind === 'resume' ? 'resume_json' : 'cover_text', version_number: 1,
    content_json: JSON.stringify(kind === 'resume' ? { profile: savedProfile } : { body: 'Complete original cover body' }),
  })
}

it('edits skills and role content in a new version, preserving untouched fields', async () => {
  mockSavedDocument('resume')
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><Documents /></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Edit → new version' }))
  expect(await screen.findByText('Edit content — saves as a new version')).toBeInTheDocument()
  expect(screen.getByLabelText('Summary')).toHaveValue('Original summary')
  expect(screen.getByLabelText(/Skills \(one per line\)/)).toHaveValue('Go\nOperations')
  expect(screen.getByLabelText(/Responsibilities and achievements/)).toHaveValue('Delivered the project')
  expect(screen.getByLabelText('Job title')).toHaveValue('Lead')
  fireEvent.change(screen.getByLabelText(/Skills \(one per line\)/), { target: { value: 'Go\nLeadership' } })
  fireEvent.change(screen.getByLabelText(/Responsibilities and achievements/), { target: { value: 'Delivered the project\nReduced costs by 20%' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save new version' }))
  await waitFor(() => expect(documentsApi.appendResumeVersion).toHaveBeenCalledWith('doc1', expect.objectContaining({
    profile: { ...savedProfile, skills: ['Go', 'Leadership'], experience_details: [{ ...savedProfile.experience_details![0],
      key_responsibilities: ['Delivered the project', 'Reduced costs by 20%'] }] },
  })))
  expect(savedProfile.skills).toEqual(['Go', 'Operations'])
})

it('keeps the full cover body editor and saves a new cover version', async () => {
  mockSavedDocument('cover_letter')
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><Documents /></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Edit → new version' }))
  const body = await screen.findByDisplayValue('Complete original cover body')
  fireEvent.change(body, { target: { value: 'Updated full cover body' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save new version' }))
  await waitFor(() => expect(documentsApi.appendCoverVersion).toHaveBeenCalledWith('doc1', expect.objectContaining({ body: 'Updated full cover body' })))
})


it('confirms deletion, warns about a default, and refreshes the saved count', async () => {
  mockSavedDocument('resume', true)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><Documents /></QueryClientProvider>)
  expect(await screen.findByText('Saved documents (1)')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Delete Saved document' }))
  expect(await screen.findByRole('dialog', { name: 'Delete document?' })).toBeInTheDocument()
  expect(screen.getByText(/Deleting it clears that default/)).toBeInTheDocument()
  expect(documentsApi.deleteDocument).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(documentsApi.deleteDocument).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Delete Saved document' }))
  fireEvent.click(screen.getByRole('button', { name: 'Delete document' }))
  await waitFor(() => expect(documentsApi.deleteDocument).toHaveBeenCalledWith('doc1'))
  expect(await screen.findByText('Saved documents (0)')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Delete Saved document' })).not.toBeInTheDocument()
})

it('keeps the document and shows deletion failures', async () => {
  mockSavedDocument('cover_letter')
  vi.mocked(documentsApi.deleteDocument).mockRejectedValueOnce(new Error('Could not delete document'))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={qc}><Documents /></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Delete Saved document' }))
  fireEvent.click(screen.getByRole('button', { name: 'Delete document' }))
  await waitFor(() => expect(screen.getAllByRole('alert').some(el => el.textContent === 'Could not delete document')).toBe(true))
  expect(screen.getByText('Saved documents (1)')).toBeInTheDocument()
  expect(screen.getByRole('dialog')).toBeInTheDocument()
})
