import { ExternalLink } from 'lucide-react'

function linkTarget(value: string, doi: boolean): string | null {
  let raw = value.trim()
  if (!raw || /\s/.test(raw)) return null
  if (doi) {
    raw = raw.replace(/^doi:/i, '')
    if (raw.startsWith('10.') && raw.includes('/')) {
      const url = new URL('https://doi.org')
      url.pathname = '/' + raw
      return url.href
    }
    if (!/^https?:\/\//i.test(raw) && !raw.startsWith('doi.org/')) return null
  }
  if (raw.startsWith('//')) raw = 'https:' + raw
  else if (!/^[a-z][a-z\d+.-]*:/i.test(raw)) raw = 'https://' + raw
  try {
    const url = new URL(raw)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : null
  } catch {
    return null
  }
}

export function OpenProfileLink({ value, doi = false }: { value?: string; doi?: boolean }) {
  const href = linkTarget(value ?? '', doi)
  if (!href) return null
  return (
    <a href={href} target="_blank" rel="noopener noreferrer"
      className="inline-flex items-center gap-1 mt-1 text-xs text-[var(--color-accent)] underline">
      Open link <ExternalLink size={12} aria-hidden="true" />
    </a>
  )
}
