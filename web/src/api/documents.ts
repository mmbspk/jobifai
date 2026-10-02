import { ApiError, apiFetch, getToken } from './client'

const API_BASE = (import.meta.env.VITE_API_BASE ?? '') + '/api'

export type DocumentVersion = {
  id: string
  version_number: number
  source: string
  content_kind: string
  market?: string
  reconstructible: boolean
  has_pdf: boolean
  created_at: string
}

export type UserDocument = {
  id: string
  kind: 'resume' | 'cover_letter' | 'original_upload'
  title: string
  created_at: string
  updated_at: string
  versions?: DocumentVersion[]
}

export type DocumentDefaults = {
  resume_version_id?: string
  cover_letter_version_id?: string
  resume_outdated?: boolean
  cover_outdated?: boolean
  outdated_reason?: string
}

export type DocumentsListResponse = {
  documents: UserDocument[]
  defaults: DocumentDefaults
}

export const documentsApi = {
  list: () => apiFetch<DocumentsListResponse>('/documents'),

  createResumeFromProfile: (body: { title?: string; market?: string; style?: string }) =>
    apiFetch<{ content_version_id: string }>('/documents/resume/from-profile', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  saveCoverLetter: (body: { title?: string; body: string; market?: string; style?: string }) =>
    apiFetch<{ content_version_id: string }>('/documents/cover-letter', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  aiImproveResume: (body: { title?: string; market?: string; style?: string }) =>
    apiFetch<{ content_version_id: string }>('/documents/resume/ai-improve', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  aiGenerateCover: (body: { title?: string; market?: string; style?: string }) =>
    apiFetch<{ content_version_id: string }>('/documents/cover-letter/ai-generate', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  setDefault: (kind: 'resume' | 'cover_letter', content_version_id: string) =>
    apiFetch<{ message: string }>('/documents/defaults', {
      method: 'PUT',
      body: JSON.stringify({ kind, content_version_id }),
    }),

}

export async function fetchDocumentPdf(versionId: string): Promise<Blob> {
  const token = getToken()
  const res = await fetch(`${API_BASE}/documents/versions/${versionId}/pdf?inline=1`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = await res.json()
      msg = j.message ?? msg
    } catch { /* ignore */ }
    throw new ApiError(res.status, msg)
  }
  return res.blob()
}
