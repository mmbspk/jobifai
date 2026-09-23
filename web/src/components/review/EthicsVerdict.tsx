import { useState } from 'react'
import * as Collapsible from '@radix-ui/react-collapsible'
import { ChevronDown } from 'lucide-react'
import { cn } from '../../lib'
import type { HalalVerdict } from '../../types'
import { Badge } from '../ui/badge'

const VERDICT_LABEL: Record<HalalVerdict['verdict'], string> = {
  HALAL: 'Likely permissible',
  DOUBTFUL: 'Potential concern',
  HARAM: 'Likely incompatible',
}

const VERDICT_VARIANT: Record<HalalVerdict['verdict'], 'success' | 'warn' | 'danger'> = {
  HALAL: 'success',
  DOUBTFUL: 'warn',
  HARAM: 'danger',
}

interface EthicsVerdictProps {
  readonly verdict: HalalVerdict
  readonly defaultOpen?: boolean
  readonly className?: string
}

export function EthicsVerdict({ verdict, defaultOpen = false, className }: EthicsVerdictProps) {
  const [open, setOpen] = useState(defaultOpen || verdict.verdict === 'DOUBTFUL')
  const friendly = VERDICT_LABEL[verdict.verdict]

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className={cn('rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-2)]/50', className)}>
      <Collapsible.Trigger className="flex w-full items-center justify-between gap-2 px-3 py-2.5 text-left">
        <div>
          <p className="text-xs text-[var(--color-text-dim)]">Employment ethics</p>
          <div className="flex flex-wrap items-center gap-2 mt-0.5">
            <Badge variant={VERDICT_VARIANT[verdict.verdict]}>{friendly}</Badge>
            <span className="text-[0.65rem] text-[var(--color-text-dim)] uppercase tracking-wide">{verdict.verdict}</span>
          </div>
        </div>
        <ChevronDown size={14} className={cn('text-[var(--color-text-dim)] shrink-0 transition-transform', open && 'rotate-180')} />
      </Collapsible.Trigger>
      <Collapsible.Content className="px-3 pb-3 space-y-2 border-t border-[var(--color-border-subtle)]">
        <p className="text-sm text-[var(--color-text-muted)] pt-2">{verdict.summary}</p>
        {verdict.reasons.length > 0 && (
          <ul className="text-xs text-[var(--color-text-dim)] space-y-1 list-disc pl-4">
            {verdict.reasons.map(r => <li key={r}>{r}</li>)}
          </ul>
        )}
        {verdict.caveats && (
          <p className="text-xs text-[var(--color-text-dim)] italic">{verdict.caveats}</p>
        )}
        {verdict.scholar_note && (
          <p className="text-xs text-[var(--color-text-dim)]">{verdict.scholar_note}</p>
        )}
        <p className="text-[0.65rem] text-[var(--color-text-dim)] leading-relaxed pt-1">
          Automated screening aid, not a religious ruling. Consider qualified guidance where needed.
        </p>
      </Collapsible.Content>
    </Collapsible.Root>
  )
}
