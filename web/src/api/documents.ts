import { ApiError, apiGet, apiPost, apiPut, getToken } from './client'
import type { ResumeProfile } from '../types'

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

export type VersionDetail = {
  id: string
  document_id: string
  content_kind: string
  content_json: string
  version_number: number
}

export const documentsApi = {
  list: () => apiGet<DocumentsListResponse>('/documents'),

  getVersion: (versionId: string) => apiGet<VersionDetail>(`/documents/versions/${versionId}`),

  createResumeFromProfile: (body: { title?: string; market?: string; style?: string; document_id?: string }) =>
    apiPost<{ content_version_id: string }>('/documents/resume/from-profile', body),

  appendResumeVersion: (documentId: string, body: { title?: string; market?: string; style?: string; profile: ResumeProfile }) =>
    apiPost<{ content_version_id: string }>(`/documents/${documentId}/resume-versions`, body),

  saveCoverLetter: (body: { title?: string; body: string; market?: string; style?: string; document_id?: string }) =>
    apiPost<{ content_version_id: string }>('/documents/cover-letter', body),

  appendCoverVersion: (documentId: string, body: { title?: string; body: string; market?: string; style?: string }) =>
    apiPost<{ content_version_id: string }>(`/documents/${documentId}/cover-versions`, body),

  aiImproveResume: (body: { title?: string; market?: string; style?: string; document_id?: string }) =>
    apiPost<{ content_version_id: string }>('/documents/resume/ai-improve', body),

  aiGenerateCover: (body: { title?: string; market?: string; style?: string; document_id?: string }) =>
    apiPost<{ content_version_id: string }>('/documents/cover-letter/ai-generate', body),

  setDefault: (kind: 'resume' | 'cover_letter', content_version_id: string) =>
    apiPut<{ message: string }>('/documents/defaults', { kind, content_version_id }),
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

export async function fetchDocumentOriginal(versionId: string): Promise<Blob> {
  const token = getToken()
  const res = await fetch(`${API_BASE}/documents/versions/${versionId}/original`, {
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
