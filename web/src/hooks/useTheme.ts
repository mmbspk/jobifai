import { useState, useEffect } from 'react'

type Theme = 'dark' | 'light'
type Listener = (t: Theme) => void

// Module-level state — survives React re-renders and context changes.
let current: Theme = (() => {
  const saved = localStorage.getItem('theme')
  if (saved === 'dark' || saved === 'light') return saved
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
})()

const listeners = new Set<Listener>()

function applyTheme(t: Theme) {
  current = t
  document.documentElement.dataset.theme = t
  localStorage.setItem('theme', t)
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', t === 'dark' ? '#09090b' : '#f6f6f7')
  listeners.forEach(fn => fn(t))
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(current)

  useEffect(() => {
    // Sync in case module re-initialized while component was mounted
    setTheme(current)
    listeners.add(setTheme)
    return () => { listeners.delete(setTheme) }
  }, [])

  return {
    theme,
    toggle: () => applyTheme(current === 'dark' ? 'light' : 'dark'),
  }
}
