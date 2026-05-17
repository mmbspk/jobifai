import { describe, test, expect } from 'vitest'
import { cn, scoreColor, scoreBg, relativeTime, formatDate } from './lib'

describe('cn', () => {
  test('merges class names', () => {
    expect(cn('a', 'b')).toBe('a b')
  })

  test('filters falsy values', () => {
    expect(cn('a', false, undefined, 'b')).toBe('a b')
  })

  test('resolves Tailwind conflicts — last class wins', () => {
    expect(cn('text-red-400', 'text-blue-400')).toBe('text-blue-400')
  })

  test('handles empty input', () => {
    expect(cn()).toBe('')
  })

  test('handles array of classes', () => {
    expect(cn(['foo', 'bar'])).toBe('foo bar')
  })
})

describe('scoreColor', () => {
  test('returns an hsl string', () => {
    expect(scoreColor(5)).toMatch(/^hsl\(\d+, 70%, 55%\)$/)
  })

  test('score 0 maps to hue 0 (red)', () => {
    expect(scoreColor(0)).toBe('hsl(0, 70%, 55%)')
  })

  test('score 10 maps to hue 120 (green)', () => {
    expect(scoreColor(10)).toBe('hsl(120, 70%, 55%)')
  })

  test('score 5 maps to hue 60 (amber)', () => {
    expect(scoreColor(5)).toBe('hsl(60, 70%, 55%)')
  })
})

describe('scoreBg', () => {
  test('returns an hsla string', () => {
    expect(scoreBg(5)).toMatch(/^hsla\(\d+, 70%, 55%, 0\.15\)$/)
  })

  test('score 0 maps to hue 0', () => {
    expect(scoreBg(0)).toBe('hsla(0, 70%, 55%, 0.15)')
  })

  test('score 10 maps to hue 120', () => {
    expect(scoreBg(10)).toBe('hsla(120, 70%, 55%, 0.15)')
  })
})

describe('relativeTime', () => {
  test('just now for less than 60s ago', () => {
    const iso = new Date(Date.now() - 10_000).toISOString()
    expect(relativeTime(iso)).toBe('just now')
  })

  test('minutes ago', () => {
    const iso = new Date(Date.now() - 2 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('2m ago')
  })

  test('hours ago', () => {
    const iso = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('3h ago')
  })

  test('days ago', () => {
    const iso = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('3d ago')
  })

  test('weeks ago', () => {
    const iso = new Date(Date.now() - 14 * 24 * 60 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('2w ago')
  })

  test('months ago', () => {
    // 90 days → 3 months (floor(90/30)=3, < 12)
    const iso = new Date(Date.now() - 90 * 24 * 60 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('3mo ago')
  })

  test('years ago', () => {
    const iso = new Date(Date.now() - 2 * 365 * 24 * 60 * 60 * 1000).toISOString()
    expect(relativeTime(iso)).toBe('2y ago')
  })
})

describe('formatDate', () => {
  test('returns a non-empty string', () => {
    const result = formatDate('2026-05-07T10:30:00Z')
    expect(typeof result).toBe('string')
    expect(result.length).toBeGreaterThan(0)
  })

  test('includes the year', () => {
    const result = formatDate('2026-05-07T10:30:00Z')
    expect(result).toContain('2026')
  })
})
