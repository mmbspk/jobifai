import type { ReactNode } from 'react'
import { cn } from '../../lib'

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  fullWidth?: boolean
  loading?: boolean
  leftIcon?: ReactNode
}

const spinner = (
  <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden>
    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" />
  </svg>
)

export function Button({
  variant = 'primary',
  size = 'md',
  fullWidth = false,
  loading = false,
  leftIcon,
  children,
  className,
  disabled,
  ...props
}: ButtonProps) {
  const variantClass = {
    primary: cn(
      'bg-[var(--color-accent)] text-white font-semibold',
      'hover:bg-[var(--color-accent-hover)]',
      'shadow-[var(--shadow-sm)]',
      'active:scale-[0.98]',
      'disabled:opacity-50 disabled:cursor-not-allowed disabled:active:scale-100',
    ),
    secondary: cn(
      'bg-[var(--color-surface)] border border-[var(--color-border)] text-[var(--color-text)] font-medium',
      'hover:bg-[var(--color-surface-2)] hover:border-[var(--color-border-strong)]',
      'disabled:opacity-50 disabled:cursor-not-allowed',
    ),
    ghost: cn(
      'bg-transparent text-[var(--color-text-muted)] font-medium',
      'hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]',
      'disabled:opacity-50 disabled:cursor-not-allowed',
    ),
    danger: cn(
      'bg-[var(--color-danger-soft)] border border-[var(--color-danger)]/25 text-[var(--color-danger)] font-medium',
      'hover:bg-[var(--color-danger)]/15',
      'disabled:opacity-50 disabled:cursor-not-allowed',
    ),
  }[variant]

  const sizeClass = {
    sm: 'text-xs h-8 px-2.5 rounded-[var(--radius-sm)] gap-1.5',
    md: 'text-sm h-10 px-4 rounded-[var(--radius-md)] gap-2 min-h-[44px] sm:min-h-0',
    lg: 'text-sm h-11 px-5 rounded-[var(--radius-md)] gap-2 min-h-[44px]',
  }[size]

  return (
    <button
      disabled={disabled || loading}
      className={cn(
        'inline-flex items-center justify-center transition-all',
        sizeClass,
        variantClass,
        fullWidth && 'w-full',
        className,
      )}
      {...props}
    >
      {loading ? spinner : leftIcon}
      {children}
    </button>
  )
}
