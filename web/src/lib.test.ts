import { describe, test, expect } from 'vitest'
import { cn, scoreColor, scoreBg, scoreBand, scoreBandLabel, relativeTime, formatDate, formatPostedDisplay } from './lib'

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

describe('scoreBand', () => {
  test('maps scores to semantic bands', () => {
    expect(scoreBand(2)).toBe('low')
    expect(scoreBand(5)).toBe('moderate')
    expect(scoreBand(7.5)).toBe('strong')
    expect(scoreBand(9)).toBe('excellent')
  })

  test('labels bands for display', () => {
    expect(scoreBandLabel('strong')).toBe('Strong')
  })
})

describe('scoreColor', () => {
  test('returns CSS variable references', () => {
    expect(scoreColor(2)).toBe('var(--color-danger)')
    expect(scoreColor(9)).toBe('var(--color-success)')
  })
})

describe('scoreBg', () => {
  test('returns soft background tokens', () => {
    expect(scoreBg(5)).toBe('var(--color-warn-soft)')
    expect(scoreBg(8)).toBe('var(--color-accent-soft)')
  })
})

describe('formatPostedDisplay', () => {
  test('uses ISO posted date', () => {
    const iso = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString()
    const out = formatPostedDisplay(iso, '2026-01-01T00:00:00Z')
    expect(out.label).toBe('2d ago')
  })

  test('falls back to created_at when posted is listing text', () => {
    const created = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString()
    const out = formatPostedDisplay('3 days ago', created)
    expect(out.label).toBe('3h ago')
  })

  test('shows raw listing text when neither is parseable', () => {
    const out = formatPostedDisplay('Posted 1 week ago', '')
    expect(out.label).toBe('Posted 1 week ago')
  })
})

describe('relativeTime', () => {
  test('invalid input returns em dash', () => {
    expect(relativeTime('3 days ago')).toBe('—')
  })
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
