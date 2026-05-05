import { cn } from '../lib'
import type { Platform } from '../types'

const PLATFORM_STYLES: Record<Platform | string, { bg: string; text: string; label: string }> = {
  linkedin: { bg: 'bg-[#0077b5]/15', text: 'text-[#0aa3e0]', label: 'LinkedIn' },
  seek:     { bg: 'bg-emerald-500/10', text: 'text-emerald-400', label: 'Seek' },
  all:      { bg: 'bg-violet-500/10', text: 'text-violet-400',  label: 'All' },
}

interface Props {
  platform: string
  size?: 'sm' | 'md'
}

export function PlatformBadge({ platform, size = 'md' }: Props) {
  const style = PLATFORM_STYLES[platform] ?? PLATFORM_STYLES.all
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full font-medium',
        style.bg, style.text,
        size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-xs',
      )}
    >
      {style.label}
    </span>
  )
}
