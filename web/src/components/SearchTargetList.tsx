import { useEffect, useRef, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { cn } from '../lib'
import { apiGet } from '../api/client'
import type { SearchTarget } from '../types'

function WorkTypeToggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-1.5 cursor-pointer select-none text-xs text-[var(--color-text-muted)]">
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.target.checked)}
        className="rounded border-[var(--color-border)] accent-[var(--color-accent)]"
      />
      {label}
    </label>
  )
}

function LocationSuggestInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [input, setInput] = useState(value)
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [activeIdx, setActiveIdx] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { setInput(value) }, [value])

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

  function pick(v: string) {
    onChange(v.trim())
    setInput(v.trim())
    setSuggestions([])
    setActiveIdx(-1)
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (suggestions.length > 0) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setActiveIdx(i => Math.min(i + 1, suggestions.length - 1)); return }
      if (e.key === 'ArrowUp') { e.preventDefault(); setActiveIdx(i => Math.max(i - 1, -1)); return }
      if (e.key === 'Escape') { setSuggestions([]); setActiveIdx(-1); return }
      if ((e.key === 'Enter' || e.key === 'Tab') && activeIdx >= 0) {
        e.preventDefault()
        pick(suggestions[activeIdx])
        return
      }
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      pick(input)
    }
  }

  return (
    <div ref={containerRef} className="relative flex-1 min-w-0">
      <input
        aria-label="Search location"
        value={input}
        onChange={e => { setInput(e.target.value); onChange(e.target.value) }}
        onKeyDown={onKeyDown}
        onBlur={() => { setTimeout(() => setSuggestions([]), 150) }}
        placeholder="Adelaide, Melbourne, All Melbourne VIC…"
        className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-2.5 py-1.5 text-xs text-[var(--color-text)] outline-none placeholder:text-[var(--color-text-dim)]"
      />
      {suggestions.length > 0 && (
        <ul className="absolute z-50 left-0 right-0 mt-1 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg shadow-lg overflow-hidden">
          {suggestions.map((s, i) => (
            <li
              key={s}
              onMouseDown={() => pick(s)}
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

interface Props {
  targets: SearchTarget[]
  onChange: (targets: SearchTarget[]) => void
}

export function SearchTargetList({ targets, onChange }: Props) {
  function update(idx: number, patch: Partial<SearchTarget>) {
    onChange(targets.map((t, i) => (i === idx ? { ...t, ...patch } : t)))
  }

  function remove(idx: number) {
    onChange(targets.filter((_, i) => i !== idx))
  }

  function add() {
    onChange([...targets, { location: '', remote: false, hybrid: true, onsite: false }])
  }

  return (
    <div className="space-y-2">
      <div className="hidden sm:grid sm:grid-cols-[1fr_repeat(3,minmax(0,4rem))_2rem] gap-2 px-1 text-[10px] uppercase tracking-wide text-[var(--color-text-dim)]">
        <span>Location</span>
        <span className="text-center">Remote</span>
        <span className="text-center">Hybrid</span>
        <span className="text-center">On-site</span>
        <span />
      </div>
      {targets.map((target, idx) => (
        <div
          key={idx}
          className="grid grid-cols-1 sm:grid-cols-[1fr_repeat(3,minmax(0,4rem))_2rem] gap-2 items-center bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg p-2.5"
        >
          <LocationSuggestInput
            value={target.location ?? ''}
            onChange={v => update(idx, { location: v })}
          />
          <div className="flex sm:justify-center">
            <WorkTypeToggle label="Remote" checked={target.remote ?? false} onChange={v => update(idx, { remote: v })} />
          </div>
          <div className="flex sm:justify-center">
            <WorkTypeToggle label="Hybrid" checked={target.hybrid ?? false} onChange={v => update(idx, { hybrid: v })} />
          </div>
          <div className="flex sm:justify-center">
            <WorkTypeToggle label="On-site" checked={target.onsite ?? false} onChange={v => update(idx, { onsite: v })} />
          </div>
          <button
            type="button"
            aria-label="Remove location search"
            onClick={() => remove(idx)}
            disabled={targets.length <= 1}
            className="justify-self-end p-1 text-[var(--color-text-dim)] hover:text-[var(--color-danger)] disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <X size={14} />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={add}
        className="inline-flex items-center gap-1.5 text-xs text-[var(--color-accent)] hover:underline"
      >
        <Plus size={12} />
        Add location search
      </button>
      <p className="text-[10px] text-[var(--color-text-dim)]">
        Used by Seek and LinkedIn. Enter any city or region — formatted automatically per platform. Leave empty for nationwide search. Job titles below apply to every location bundle.
      </p>
    </div>
  )
}
