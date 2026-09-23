import { cn } from '../../lib'

export interface NativeSelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
}

export function NativeSelect({ label, className, id, children, ...props }: NativeSelectProps) {
  const selectId = id ?? (label ? label.toLowerCase().replace(/\s+/g, '-') : undefined)

  return (
    <div className="space-y-1.5">
      {label && (
        <label htmlFor={selectId} className="text-sm font-medium text-[var(--color-text)]">
          {label}
        </label>
      )}
      <select
        id={selectId}
        className={cn(
          'h-10 w-full bg-[var(--color-surface)] border border-[var(--color-border)]',
          'rounded-[var(--radius-md)] px-3 text-sm text-[var(--color-text)]',
          'focus:outline-none focus:border-[var(--color-accent)] focus:ring-2 focus:ring-[var(--color-accent)]/20',
          className,
        )}
        {...props}
      >
        {children}
      </select>
    </div>
  )
}
