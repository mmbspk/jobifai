import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  ListChecks,
  Sparkles,
  X,
} from 'lucide-react'
import { cn } from '../../lib'
import { Button } from '../Button'
import type { EvaluatedSetupStep } from '../../lib/setupChecklist'

const DISMISS_KEY = 'jobifai:setup-guide-dismissed'
const COMPLETE_KEY = 'jobifai:setup-guide-complete'

function readDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === 'true'
  } catch {
    return false
  }
}

function persistDismissed(value: boolean) {
  try {
    if (value) localStorage.setItem(DISMISS_KEY, 'true')
    else localStorage.removeItem(DISMISS_KEY)
  } catch {
    /* ignore */
  }
}

function readComplete(): boolean {
  try {
    return localStorage.getItem(COMPLETE_KEY) === 'true'
  } catch {
    return false
  }
}

function persistComplete() {
  try {
    localStorage.setItem(COMPLETE_KEY, 'true')
    localStorage.removeItem(DISMISS_KEY)
  } catch {
    /* ignore */
  }
}

interface SetupGuideProps {
  readonly steps: EvaluatedSetupStep[]
  readonly ready: boolean
  readonly progress: { done: number; total: number }
  readonly nextStep: EvaluatedSetupStep | undefined
  readonly loading: boolean
  readonly onRefresh: () => void
}

export function SetupGuide({ steps, ready, progress, nextStep, loading, onRefresh }: SetupGuideProps) {
  const [dismissed, setDismissed] = useState(readDismissed)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [showSuccessBanner, setShowSuccessBanner] = useState(() => !readComplete())

  useEffect(() => {
    if (!ready && nextStep && expandedId === null) {
      setExpandedId(nextStep.id)
    }
  }, [ready, nextStep, expandedId])

  if (loading) return null

  if (ready && readComplete()) return null

  if (ready && showSuccessBanner) {
    return (
      <section className="rounded-[var(--radius-xl)] border border-[var(--color-success)]/35 bg-[var(--color-success-soft)] px-5 py-4 shadow-[var(--shadow-sm)]">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-success)]/15 text-[var(--color-success)]">
            <Sparkles size={18} />
          </div>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-semibold text-[var(--color-text)]">You&apos;re set up for automation</p>
            <p className="text-xs text-[var(--color-text-muted)] mt-0.5">
              Profile, search preferences, and a platform login are in place. Choose a board on the card below and start automation.
            </p>
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              persistComplete()
              setShowSuccessBanner(false)
            }}
          >
            Hide
          </Button>
        </div>
      </section>
    )
  }

  if (dismissed && !ready) {
    return (
      <button
        type="button"
        onClick={() => { setDismissed(false); persistDismissed(false); onRefresh() }}
        className="flex w-full items-center gap-3 rounded-[var(--radius-lg)] border border-dashed border-[var(--color-accent)]/40 bg-[var(--color-accent-soft)]/40 px-4 py-3 text-left transition-colors hover:border-[var(--color-accent)]/60"
      >
        <ListChecks size={18} className="shrink-0 text-[var(--color-accent)]" />
        <span className="text-sm text-[var(--color-text)]">
          Setup guide — {progress.done} of {progress.total} required steps done
        </span>
        <ChevronRight size={16} className="ml-auto text-[var(--color-text-dim)]" />
      </button>
    )
  }

  if (ready) return null

  const pct = progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0

  return (
    <section
      className={cn(
        'overflow-hidden rounded-[var(--radius-xl)] border shadow-[var(--shadow-card)]',
        'border-[var(--color-accent)]/30 bg-[linear-gradient(145deg,var(--color-surface),var(--color-surface)_55%,var(--color-accent-soft))]',
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--color-border-subtle)] px-5 py-4">
        <div className="flex gap-3 min-w-0">
          <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
            <ListChecks size={20} />
          </div>
          <div className="min-w-0">
            <p className="text-xs font-medium uppercase tracking-wide text-[var(--color-accent)]">First-time setup</p>
            <h2 className="text-lg font-semibold text-[var(--color-text)] tracking-tight">Prepare before you start automation</h2>
            <p className="mt-1 text-sm text-[var(--color-text-muted)]">
              Complete these steps so the bot can search, score roles, and apply on your behalf.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => { onRefresh() }}>
            Refresh
          </Button>
          <button
            type="button"
            aria-label="Hide setup guide"
            onClick={() => { setDismissed(true); persistDismissed(true) }}
            className="rounded-md p-2 text-[var(--color-text-dim)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]"
          >
            <X size={16} />
          </button>
        </div>
      </div>

      <div className="px-5 py-4">
        <div className="mb-4 flex items-center gap-3">
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-[var(--color-surface-2)]">
            <div
              className="h-full rounded-full bg-[var(--color-accent)] transition-all duration-500"
              style={{ width: `${pct}%` }}
            />
          </div>
          <span className="text-xs font-medium tabular-nums text-[var(--color-text-muted)]">
            {progress.done}/{progress.total}
          </span>
        </div>

        <ol className="space-y-2">
          {steps.map(step => {
            const open = expandedId === step.id
            const isNext = nextStep?.id === step.id
            return (
              <li
                key={step.id}
                className={cn(
                  'rounded-[var(--radius-lg)] border transition-colors',
                  step.complete && 'border-[var(--color-success)]/25 bg-[var(--color-success-soft)]/30',
                  !step.complete && isNext && 'border-[var(--color-accent)]/40 bg-[var(--color-surface)]',
                  !step.complete && !isNext && 'border-[var(--color-border)] bg-[var(--color-surface)]/80',
                )}
              >
                <button
                  type="button"
                  className="flex w-full items-start gap-3 px-4 py-3 text-left"
                  onClick={() => setExpandedId(open ? null : step.id)}
                  aria-expanded={open}
                >
                  <span
                    className={cn(
                      'mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-bold',
                      step.complete
                        ? 'bg-[var(--color-success)] text-white'
                        : 'border border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-text-muted)]',
                    )}
                  >
                    {step.complete ? <Check size={14} strokeWidth={3} /> : step.order}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-semibold text-[var(--color-text)]">{step.title}</span>
                      {!step.required && (
                        <span className="text-[10px] uppercase tracking-wide text-[var(--color-text-dim)]">Recommended</span>
                      )}
                      {isNext && !step.complete && (
                        <span className="text-[10px] font-medium uppercase tracking-wide text-[var(--color-accent)]">Up next</span>
                      )}
                    </span>
                    <span className="mt-0.5 block text-xs text-[var(--color-text-muted)]">{step.summary}</span>
                  </span>
                  <ChevronDown
                    size={16}
                    className={cn('shrink-0 text-[var(--color-text-dim)] transition-transform', open && 'rotate-180')}
                  />
                </button>
                {open && (
                  <div className="border-t border-[var(--color-border-subtle)] px-4 pb-4 pt-3 ml-10">
                    <ul className="space-y-2 mb-4">
                      {step.instructions.map(line => (
                        <li key={line} className="flex gap-2 text-sm text-[var(--color-text-muted)]">
                          <Circle size={6} className="mt-2 shrink-0 fill-[var(--color-accent)] text-[var(--color-accent)]" />
                          <span>{line}</span>
                        </li>
                      ))}
                    </ul>
                    <Link to={step.to}>
                      <Button variant="primary" size="sm" leftIcon={<ChevronRight size={14} />}>
                        {step.cta}
                      </Button>
                    </Link>
                  </div>
                )}
              </li>
            )
          })}
        </ol>
      </div>
    </section>
  )
}
