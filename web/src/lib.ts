import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: Parameters<typeof clsx>) {
  return twMerge(clsx(...inputs))
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

export function scoreColor(score: number): string {
  // Smooth gradient: red (0) → amber (5) → green (10)
  const h = Math.round(score * 12) // 0 → 0°, 10 → 120°
  return `hsl(${h}, 70%, 55%)`
}

export function scoreBg(score: number): string {
  const h = Math.round(score * 12)
  return `hsla(${h}, 70%, 55%, 0.15)`
}

export function relativeTime(iso: string): string {
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return '—'
  const diff = Date.now() - ms
  if (diff < 0) return 'just now'
  const s = Math.floor(diff / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 7) return `${d}d ago`
  const w = Math.floor(d / 7)
  if (w < 5) return `${w}w ago`
  const mo = Math.floor(d / 30)
  if (mo < 12) return `${mo}mo ago`
  return `${Math.floor(d / 365)}y ago`
}

export function formatDate(iso: string): string {
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return iso || '—'
  return new Date(ms).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

/** Prefer ISO posted date; fall back to queue created_at; else show raw listing text. */
export function formatPostedDisplay(posted?: string, createdAt?: string): { label: string; title: string } {
  const postedTrim = posted?.trim()
  if (postedTrim && !Number.isNaN(Date.parse(postedTrim))) {
    return { label: relativeTime(postedTrim), title: formatDate(postedTrim) }
  }
  const createdTrim = createdAt?.trim()
  if (createdTrim && !Number.isNaN(Date.parse(createdTrim))) {
    return { label: relativeTime(createdTrim), title: formatDate(createdTrim) }
  }
  if (postedTrim) return { label: postedTrim, title: postedTrim }
  return { label: '—', title: '' }
}
