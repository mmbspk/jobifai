import { useState } from 'react'
import { X } from 'lucide-react'
import { cn } from '../lib'

interface Props {
  values: string[]
  onChange: (values: string[]) => void
  placeholder?: string
  className?: string
}

export function TagInput({ values, onChange, placeholder = 'Add…', className }: Props) {
  const [input, setInput] = useState('')

  function add() {
    const v = input.trim()
    if (v && !values.includes(v)) onChange([...values, v])
    setInput('')
  }

  function remove(v: string) {
    onChange(values.filter(x => x !== v))
  }

  return (
    <div className={cn('flex flex-wrap gap-1.5', className)}>
      {values.map(v => (
        <span
          key={v}
          className="inline-flex items-center gap-1 bg-[var(--color-surface-2)] border border-[var(--color-border)] text-[var(--color-text-muted)] text-xs px-2 py-0.5 rounded-full"
        >
          {v}
          <button onClick={() => remove(v)} className="hover:text-[var(--color-danger)]">
            <X size={10} />
          </button>
        </span>
      ))}
      <input
        value={input}
        onChange={e => setInput(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add() }
        }}
        onBlur={add}
        placeholder={placeholder}
        className="bg-transparent text-xs text-[var(--color-text)] outline-none placeholder:text-[var(--color-text-dim)] min-w-24"
      />
    </div>
  )
}
