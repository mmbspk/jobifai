import { ChevronDown, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { cn } from '../../lib'
import { inputClassName } from '../ui/input'
import { Button } from '../ui/button'
import { PageHeader } from '../shell/PageHeader'

export function JobListPage({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-6">
      <PageHeader title={title} description={description} />
      {children}
    </div>
  )
}

export function JobSearchBar({
  value,
  onChange,
  placeholder = 'Search company or role…',
  children,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  children?: React.ReactNode
}) {
  return (
    <div className="flex flex-col sm:flex-row gap-2 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-sm)]">
      <input
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className={cn(inputClassName, 'flex-1')}
      />
      {children}
    </div>
  )
}

export function FilterSelect({
  value,
  onChange,
  children,
  'aria-label': ariaLabel,
}: {
  value: string
  onChange: (v: string) => void
  children: React.ReactNode
  'aria-label'?: string
}) {
  return (
    <select
      value={value}
      onChange={e => onChange(e.target.value)}
      aria-label={ariaLabel}
      className={cn(inputClassName, 'sm:w-auto sm:min-w-[140px]')}
    >
      {children}
    </select>
  )
}

export function ActiveFilterChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-[var(--color-accent-soft)] border border-[var(--color-accent)]/25 text-xs text-[var(--color-accent)] w-fit">
      <span>{label}</span>
      <button type="button" onClick={onClear} className="hover:opacity-80" aria-label={`Clear ${label} filter`}>
        <X size={12} />
      </button>
    </div>
  )
}

export function JobListPanel({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] divide-y divide-[var(--color-border-subtle)] overflow-hidden shadow-[var(--shadow-card)]">
      {children}
    </div>
  )
}

export function JobListEmpty({ message, action }: { message: string; action?: { label: string; to: string } }) {
  return (
    <div className="px-4 py-14 text-center space-y-3">
      <p className="text-sm text-[var(--color-text-muted)]">{message}</p>
      {action && (
        <Link to={action.to} className="text-sm font-medium text-[var(--color-accent)] hover:underline">
          {action.label}
        </Link>
      )}
    </div>
  )
}

export function JobListSkeleton() {
  return <div className="px-4 py-10 text-center text-sm text-[var(--color-text-dim)]">Loading…</div>
}

export function LoadMoreButton({ onClick, loading }: { onClick: () => void; loading: boolean }) {
  return (
    <div className="flex justify-center pt-2">
      <Button variant="secondary" leftIcon={<ChevronDown size={14} />} loading={loading} onClick={onClick}>
        {loading ? 'Loading…' : 'Load more'}
      </Button>
    </div>
  )
}

export function ListFooter({ shown, total }: { shown: number; total?: number }) {
  return (
    <p className="text-xs text-[var(--color-text-dim)] text-right tabular-nums">
      {total != null ? `${shown} of ${total} shown` : `${shown} shown`}
    </p>
  )
}

export function InfoBanner({ children, variant = 'info' }: { children: React.ReactNode; variant?: 'info' | 'warn' | 'accent' }) {
  const styles = {
    info: 'bg-[var(--color-info-soft)] border-[var(--color-info)]/25 text-[var(--color-text-muted)]',
    warn: 'bg-[var(--color-warn-soft)] border-[var(--color-warn)]/25 text-[var(--color-text-muted)]',
    accent: 'bg-[var(--color-accent-soft)] border-[var(--color-accent)]/25 text-[var(--color-text-muted)]',
  }[variant]
  return (
    <div className={cn('flex items-start gap-3 p-4 rounded-[var(--radius-lg)] border text-sm', styles)}>
      {children}
    </div>
  )
}
