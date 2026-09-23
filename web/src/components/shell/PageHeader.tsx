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
    <div className={cn('flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3', className)}>
      <div className="min-w-0">
        <h1 className="text-[1.625rem] leading-8 font-bold text-[var(--color-text)] tracking-tight">
          {title}
        </h1>
        {description && (
          <p className="text-sm text-[var(--color-text-dim)] mt-1.5 max-w-2xl">{description}</p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </div>
  )
}

/** Time-of-day greeting for dashboard. */
export function dashboardGreeting(): string {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 17) return 'Good afternoon'
  return 'Good evening'
}
