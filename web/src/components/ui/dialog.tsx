import * as Dialog from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '../../lib'

export const DialogRoot = Dialog.Root
export const DialogTrigger = Dialog.Trigger
export const DialogClose = Dialog.Close

interface DialogContentProps {
  readonly title: string
  readonly description?: string
  readonly children: React.ReactNode
  readonly className?: string
}

export function DialogContent({ title, description, children, className }: DialogContentProps) {
  return (
    <Dialog.Portal>
      <Dialog.Overlay className="fixed inset-0 z-50 bg-black/50" />
      <Dialog.Content
        className={cn(
          'fixed left-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2',
          'rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]',
          'p-5 shadow-[var(--shadow-popover)]',
          'focus:outline-none',
          className,
        )}
      >
        <div className="flex items-start justify-between gap-3 mb-4">
          <div>
            <Dialog.Title className="text-base font-semibold text-[var(--color-text)]">
              {title}
            </Dialog.Title>
            {description && (
              <Dialog.Description className="text-sm text-[var(--color-text-dim)] mt-1">
                {description}
              </Dialog.Description>
            )}
          </div>
          <Dialog.Close
            className="p-1 rounded-md text-[var(--color-text-dim)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]"
            aria-label="Close"
          >
            <X size={16} />
          </Dialog.Close>
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  )
}
