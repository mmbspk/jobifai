import type { ReactNode } from 'react'
import { cn } from '../../lib'

interface PageHeaderProps {
  readonly title: string
  readonly description?: string
  readonly actions?: ReactNode
  readonly className?: string
}

export function PageHeader({ title, description, actions, className }: PageHeaderProps) {
  return (
    <div className={cn('flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between', className)}>
      <div className="min-w-0">
        <h1 className="text-[1.75rem] leading-9 font-bold text-[var(--color-text)] tracking-[-0.035em] sm:text-[2rem]">
          {title}
        </h1>
        {description && (
          <p className="mt-1.5 max-w-2xl text-[0.9375rem] leading-6 text-[var(--color-text-muted)]">{description}</p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </div>
  )
}
