import { cn } from '../../lib'

type BadgeVariant = 'default' | 'accent' | 'success' | 'warn' | 'danger' | 'admin' | 'muted'

interface BadgeProps {
  readonly children: React.ReactNode
  readonly variant?: BadgeVariant
  readonly className?: string
}

const variants: Record<BadgeVariant, string> = {
  default: 'bg-[var(--color-surface-2)] text-[var(--color-text-muted)] border-[var(--color-border)]',
  accent: 'bg-[var(--color-accent-soft)] text-[var(--color-accent)] border-[var(--color-accent)]/30',
  success: 'bg-[var(--color-success-soft)] text-[var(--color-success)] border-[var(--color-success)]/25',
  warn: 'bg-[var(--color-warn-soft)] text-[var(--color-warn)] border-[var(--color-warn)]/25',
  danger: 'bg-[var(--color-danger-soft)] text-[var(--color-danger)] border-[var(--color-danger)]/25',
  admin: 'bg-[var(--color-admin-soft)] text-[var(--color-admin)] border-[var(--color-admin)]/25',
  muted: 'bg-transparent text-[var(--color-text-dim)] border-[var(--color-border)]',
}

export function Badge({ children, variant = 'default', className }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium',
        variants[variant],
        className,
      )}
    >
      {children}
    </span>
  )
}
