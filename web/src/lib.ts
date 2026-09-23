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

export type ScoreBand = 'low' | 'moderate' | 'strong' | 'excellent'

/** Semantic score bands (1–3 low, 4–6 moderate, 7–8 strong, 9–10 excellent). */
export function scoreBand(score: number): ScoreBand {
  if (score <= 3) return 'low'
  if (score <= 6) return 'moderate'
  if (score <= 8) return 'strong'
  return 'excellent'
}

export function scoreBandLabel(band: ScoreBand): string {
  switch (band) {
    case 'low': return 'Low'
    case 'moderate': return 'Moderate'
    case 'strong': return 'Strong'
    case 'excellent': return 'Excellent'
  }
}

/** Foreground color for inline use (charts); pills prefer CSS classes. */
export function scoreColor(score: number): string {
  switch (scoreBand(score)) {
    case 'low': return 'var(--color-danger)'
    case 'moderate': return 'var(--color-warn)'
    case 'strong': return 'var(--color-info)'
    case 'excellent': return 'var(--color-success)'
  }
}

export function scoreBg(score: number): string {
  switch (scoreBand(score)) {
    case 'low': return 'var(--color-danger-soft)'
    case 'moderate': return 'var(--color-warn-soft)'
    case 'strong': return 'var(--color-accent-soft)'
    case 'excellent': return 'var(--color-success-soft)'
  }
}

export function formatScore(score: number): string {
  const rounded = Math.round(score * 10) / 10
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)
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
