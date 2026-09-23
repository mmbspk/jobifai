import { cn } from '../../lib'

export const inputClassName = cn(
  'w-full h-11 sm:h-10',
  'bg-[var(--color-surface)] border border-[var(--color-border)]',
  'rounded-[var(--radius-md)] px-3 text-sm text-[var(--color-text)]',
  'placeholder:text-[var(--color-text-dim)]',
  'transition-colors',
  'focus:outline-none focus:border-[var(--color-accent)] focus:ring-2 focus:ring-[var(--color-accent)]/20',
)

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string
  helper?: string
  error?: string
  optional?: boolean
}

export function Input({ label, helper, error, optional, className, id, ...props }: InputProps) {
  const inputId = id ?? (label ? label.toLowerCase().replace(/\s+/g, '-') : undefined)

  return (
    <div className="space-y-1.5">
      {label && (
        <div className="flex items-baseline justify-between gap-2">
          <label htmlFor={inputId} className="text-sm font-medium text-[var(--color-text)]">
            {label}
          </label>
          {optional && <span className="text-xs text-[var(--color-text-dim)]">Optional</span>}
        </div>
      )}
      {helper && !error && (
        <p className="text-xs text-[var(--color-text-dim)]">{helper}</p>
      )}
      <input
        id={inputId}
        className={cn(inputClassName, error && 'border-[var(--color-danger)]', className)}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${inputId}-error` : undefined}
        {...props}
      />
      {error && (
        <p id={`${inputId}-error`} className="text-xs text-[var(--color-danger)]" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
