import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { cn } from '../../lib'
import { inputClassName } from '../ui/input'
import { Button } from '../ui/button'

interface MaskedSecretFieldProps {
  readonly configured: boolean
  readonly label: string
  readonly helper?: string
  readonly onSave: (value: string) => Promise<void>
  readonly onDelete?: () => Promise<void>
}

export function MaskedSecretField({ configured, label, helper, onSave, onDelete }: MaskedSecretFieldProps) {
  const [editing, setEditing] = useState(false)
  const [val, setVal] = useState('')
  const [show, setShow] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)

  async function save() {
    setSaving(true)
    await onSave(val)
    setSaving(false)
    setEditing(false)
    setVal('')
  }

  if (!editing) {
    return (
      <div className="space-y-1">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm text-[var(--color-text-muted)]">
            {configured ? 'Configured' : 'Not configured'}
          </span>
          <button
            type="button"
            onClick={() => setEditing(true)}
            className="text-sm font-medium text-[var(--color-accent)] hover:underline"
          >
            {configured ? 'Replace credential' : 'Set credential'}
          </button>
          {configured && onDelete && (
            <button
              type="button"
              onClick={async () => { setDeleting(true); await onDelete(); setDeleting(false) }}
              disabled={deleting}
              className="text-sm text-[var(--color-danger)] hover:underline disabled:opacity-50"
            >
              {deleting ? 'Removing…' : 'Remove'}
            </button>
          )}
        </div>
        {helper && <p className="text-xs text-[var(--color-text-dim)]">{helper}</p>}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-col sm:flex-row gap-2">
        <div className="relative flex-1">
          <input
            autoFocus
            type={show ? 'text' : 'password'}
            value={val}
            onChange={e => setVal(e.target.value)}
            placeholder={`Enter ${label}`}
            className={cn(inputClassName, 'pr-10')}
            onKeyDown={e => {
              if (e.key === 'Enter') void save()
              if (e.key === 'Escape') setEditing(false)
            }}
          />
          <button
            type="button"
            onClick={() => setShow(v => !v)}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--color-text-dim)] p-1"
            aria-label={show ? 'Hide' : 'Show'}
          >
            {show ? <EyeOff size={14} /> : <Eye size={14} />}
          </button>
        </div>
        <Button variant="primary" size="sm" disabled={!val || saving} loading={saving} onClick={() => void save()}>
          Save
        </Button>
        <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>Cancel</Button>
      </div>
      <p className="text-xs text-[var(--color-text-dim)]">Stored securely — the previous value is never shown again.</p>
    </div>
  )
}
