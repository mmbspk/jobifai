import { cn } from '../lib'

const LABELS: Record<string, string> = {
  linkedin: 'LinkedIn',
  seek: 'Seek',
}

interface Props {
  platform: string
  size?: 'sm' | 'md'
}

/** Text-first neutral platform badge (no third-party logo marks). */
export function PlatformBadge({ platform, size = 'md' }: Props) {
  const label = LABELS[platform] ?? platform.charAt(0).toUpperCase() + platform.slice(1)
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border font-medium',
        'bg-[var(--color-surface-2)] text-[var(--color-text-muted)] border-[var(--color-border)]',
        size === 'sm' ? 'px-2 py-0.5 text-[0.65rem]' : 'px-2.5 py-0.5 text-xs',
      )}
    >
      {label}
    </span>
  )
}
