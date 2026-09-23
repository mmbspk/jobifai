import * as RadixSwitch from '@radix-ui/react-switch'
import { cn } from '../../lib'

interface SwitchProps {
  readonly checked: boolean
  readonly onCheckedChange: (checked: boolean) => void
  readonly label?: string
  readonly helper?: string
  readonly disabled?: boolean
  readonly id?: string
}

export function Switch({ checked, onCheckedChange, label, helper, disabled, id }: SwitchProps) {
  const switchId = id ?? (label ? label.toLowerCase().replace(/\s+/g, '-') : undefined)

  return (
    <div className="flex items-start justify-between gap-4">
      {(label || helper) && (
        <div className="min-w-0">
          {label && (
            <label htmlFor={switchId} className="text-sm text-[var(--color-text)]">
              {label}
            </label>
          )}
          {helper && <p className="text-xs text-[var(--color-text-dim)] mt-0.5">{helper}</p>}
        </div>
      )}
      <RadixSwitch.Root
        id={switchId}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        className={cn(
          'relative w-10 h-[22px] shrink-0 rounded-full transition-colors',
          'data-[state=checked]:bg-[var(--color-accent)] data-[state=unchecked]:bg-[var(--color-border)]',
          'disabled:opacity-50 disabled:cursor-not-allowed',
        )}
      >
        <RadixSwitch.Thumb
          className={cn(
            'block w-[18px] h-[18px] bg-white rounded-full shadow transition-transform',
            'translate-x-0.5 data-[state=checked]:translate-x-[18px]',
          )}
        />
      </RadixSwitch.Root>
    </div>
  )
}
