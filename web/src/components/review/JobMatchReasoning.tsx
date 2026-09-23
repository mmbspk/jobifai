import * as Collapsible from '@radix-ui/react-collapsible'
import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { cn } from '../../lib'

interface JobMatchReasoningProps {
  readonly reasoning?: string
  readonly className?: string
}

/** Presents suitability reasoning with a scannable heading. */
export function JobMatchReasoning({ reasoning, className }: JobMatchReasoningProps) {
  const [open, setOpen] = useState(true)
  if (!reasoning?.trim()) return null

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className={className}>
      <Collapsible.Trigger className="flex items-center gap-1.5 text-sm font-medium text-[var(--color-text)] mb-2">
        <ChevronDown size={14} className={cn('text-[var(--color-text-dim)] transition-transform', open && 'rotate-180')} />
        Why this matches
      </Collapsible.Trigger>
      <Collapsible.Content>
        <p className="text-sm text-[var(--color-text-muted)] leading-relaxed whitespace-pre-wrap">{reasoning.trim()}</p>
      </Collapsible.Content>
    </Collapsible.Root>
  )
}
