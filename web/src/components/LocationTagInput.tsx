import { useState, useEffect, useRef } from 'react'
import { X } from 'lucide-react'
import { cn } from '../lib'
import { apiGet } from '../api/client'

interface Props {
  values: string[]
  onChange: (values: string[]) => void
}

export function LocationTagInput({ values, onChange }: Props) {
  const [input, setInput] = useState('')
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [activeIdx, setActiveIdx] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (input.length < 2) { setSuggestions([]); setActiveIdx(-1); return }
    debounceRef.current = setTimeout(async () => {
      try {
        const data = await apiGet<string[]>(`/settings/locations/suggest?q=${encodeURIComponent(input)}`)
        setSuggestions(data ?? [])
        setActiveIdx(-1)
      } catch {
        setSuggestions([])
      }
    }, 300)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [input])

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setSuggestions([])
      }
    }
    document.addEventListener('mousedown', onClickOutside)
    return () => document.removeEventListener('mousedown', onClickOutside)
  }, [])

  function add(v: string) {
    const trimmed = v.trim()
    if (trimmed && !values.includes(trimmed)) onChange([...values, trimmed])
    setInput('')
    setSuggestions([])
    setActiveIdx(-1)
  }

  function remove(v: string) {
    onChange(values.filter(x => x !== v))
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (suggestions.length > 0) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setActiveIdx(i => Math.min(i + 1, suggestions.length - 1)); return }
      if (e.key === 'ArrowUp') { e.preventDefault(); setActiveIdx(i => Math.max(i - 1, -1)); return }
      if (e.key === 'Escape') { setSuggestions([]); setActiveIdx(-1); return }
      if ((e.key === 'Enter' || e.key === 'Tab') && activeIdx >= 0) {
        e.preventDefault()
        add(suggestions[activeIdx])
        return
      }
    }
    if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(input) }
  }

  return (
    <div ref={containerRef} className="relative">
      <div className="flex flex-wrap gap-1.5">
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
          aria-label="Add location"
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => { setTimeout(() => setSuggestions([]), 150) }}
          placeholder="Add location…"
          className="bg-transparent text-xs text-[var(--color-text)] outline-none placeholder:text-[var(--color-text-dim)] min-w-24"
        />
      </div>
      {suggestions.length > 0 && (
        <ul className="absolute z-50 left-0 right-0 mt-1 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg shadow-lg overflow-hidden">
          {suggestions.map((s, i) => (
            <li
              key={s}
              onMouseDown={() => add(s)}
              className={cn(
                'px-3 py-2 text-xs cursor-pointer text-[var(--color-text)]',
                i === activeIdx ? 'bg-violet-500/20 text-violet-300' : 'hover:bg-[var(--color-surface-2)]',
              )}
            >
              {s}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
