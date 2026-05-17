import { renderHook, act } from '@testing-library/react'
import { test, expect, beforeEach, vi } from 'vitest'

// useTheme.ts accesses localStorage and matchMedia in a module-level IIFE,
// before jsdom is fully ready. vi.hoisted installs these stubs first.
vi.hoisted(() => {
  const store: Record<string, string> = {}
  ;(globalThis as Record<string, unknown>).localStorage = {
    getItem: (k: string): string | null => store[k] ?? null,
    setItem: (k: string, v: string): void => { store[k] = v },
    removeItem: (k: string): void => { delete store[k] },
    clear: (): void => { Object.keys(store).forEach(k => { delete store[k] }) },
    key: (_i: number): null => null,
    length: 0,
  }
  ;(globalThis as Record<string, unknown>).matchMedia = (_query: string) => ({
    matches: false,
    media: _query,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
})

import { useTheme } from './useTheme'

beforeEach(() => {
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

test('returns a valid theme value', () => {
  const { result } = renderHook(() => useTheme())
  expect(['dark', 'light']).toContain(result.current.theme)
})

test('toggle flips the theme', () => {
  const { result } = renderHook(() => useTheme())
  const initial = result.current.theme
  act(() => result.current.toggle())
  expect(result.current.theme).toBe(initial === 'dark' ? 'light' : 'dark')
})

test('toggle updates document.documentElement.dataset.theme', () => {
  const { result } = renderHook(() => useTheme())
  const initial = result.current.theme
  act(() => result.current.toggle())
  expect(document.documentElement.dataset.theme).toBe(initial === 'dark' ? 'light' : 'dark')
})

test('toggle persists choice to localStorage', () => {
  const { result } = renderHook(() => useTheme())
  const initial = result.current.theme
  act(() => result.current.toggle())
  expect(localStorage.getItem('theme')).toBe(initial === 'dark' ? 'light' : 'dark')
})

test('toggling twice reverts to the original theme', () => {
  const { result } = renderHook(() => useTheme())
  const initial = result.current.theme
  act(() => result.current.toggle())
  act(() => result.current.toggle())
  expect(result.current.theme).toBe(initial)
})
