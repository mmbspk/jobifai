import { ApiError, getToken } from './client'
import { applicationFileApiPath } from '../lib'

const FILES_BASE = (import.meta.env.VITE_API_BASE ?? '') + '/api/files'

/** Fetch a prepared application PDF (review preview) with Bearer auth. */
export async function fetchApplicationPreviewFile(storedPath: string): Promise<Blob> {
  const suffix = applicationFileApiPath(storedPath)
  if (!suffix) {
    throw new ApiError(400, 'missing file path')
  }
  const token = getToken()
  const res = await fetch(`${FILES_BASE}/${suffix}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = (await res.json()) as { message?: string }
      msg = j.message ?? msg
    } catch {
      /* plain-text error body */
    }
    throw new ApiError(res.status, msg)
  }
  const blob = await res.blob()
  if (blob.size === 0) {
    throw new ApiError(502, 'preview file is empty')
  }
  return blob
}

export function openApplicationPreviewBlob(blob: Blob): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.target = '_blank'
  link.rel = 'noopener noreferrer'
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 120_000)
}
