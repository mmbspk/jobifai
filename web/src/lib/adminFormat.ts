export type AdminPeriod = 'today' | '7d' | '30d' | 'all'

export const ADMIN_PERIODS: { value: AdminPeriod; label: string }[] = [
  { value: 'today', label: 'Today' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
  { value: 'all', label: 'All time' },
]

export function usdFromMicro(micro: number | undefined | null): string {
  if (micro == null) return '—'
  return (micro / 1_000_000).toFixed(4)
}

export function pct(rate: number | undefined | null): string {
  if (rate == null || Number.isNaN(rate)) return '—'
  return `${(rate * 100).toFixed(1)}%`
}

export function shortModel(provider: string, model: string): string {
  const m = model.replace(/^claude-/, '').replace(/-202\d+$/, '')
  const p = provider ? `${provider}/` : ''
  return `${p}${m || model}`
}
