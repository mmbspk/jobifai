import type { ReactNode } from 'react'
import { cn } from '../lib'

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  fullWidth?: boolean
  loading?: boolean
  leftIcon?: ReactNode
}

const spinner = (
  <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
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
    primary: [
      'bg-[var(--color-accent)] text-white font-medium',
      'hover:bg-[var(--color-accent-dim)]',
      'shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]',
      'active:scale-[0.98]',
      'disabled:opacity-50 disabled:cursor-not-allowed disabled:shadow-none disabled:active:scale-100',
      'transition-all',
    ].join(' '),
    secondary: [
      'bg-transparent border border-[var(--color-border)] text-[var(--color-text-muted)] font-medium',
      'hover:border-[var(--color-accent)]/50 hover:text-[var(--color-text)] hover:bg-[var(--color-surface-2)]',
      'disabled:opacity-50 disabled:cursor-not-allowed',
      'transition-all',
    ].join(' '),
    ghost: [
      'bg-transparent text-[var(--color-text-muted)]',
      'hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]',
      'disabled:opacity-50 disabled:cursor-not-allowed',
      'transition-colors',
    ].join(' '),
    danger: [
      'bg-red-500/10 border border-red-500/20 text-red-400 font-medium',
      'hover:bg-red-500/20 hover:border-red-500/40',
      'disabled:opacity-50 disabled:cursor-not-allowed',
      'transition-all',
    ].join(' '),
  }[variant]

  const sizeClass = {
    sm: 'text-xs px-2.5 py-1.5 rounded-lg gap-1.5',
    md: 'text-sm px-4 py-2 rounded-lg gap-2',
    lg: 'text-sm px-5 py-2.5 rounded-xl gap-2',
  }[size]

  return (
    <button
      disabled={disabled || loading}
      className={cn(
        'inline-flex items-center justify-center',
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
