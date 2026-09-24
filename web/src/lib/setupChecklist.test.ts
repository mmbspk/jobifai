import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.hoisted(() => {
  const store: Record<string, string> = {}
  ;(globalThis as Record<string, unknown>).localStorage = {
    getItem: (k: string): string | null => store[k] ?? null,
    setItem: (k: string, v: string): void => {
      store[k] = v
    },
    removeItem: (k: string): void => {
      delete store[k]
    },
    clear: (): void => {
      Object.keys(store).forEach(k => {
        delete store[k]
      })
    },
    key: (): null => null,
    length: 0,
  }
})
import {
  SETUP_PLAN_REVIEW_KEY,
  evaluateSetupSteps,
  isPlanReviewReady,
  isPlatformReady,
  isProfileReady,
  isSearchReady,
  markPlanReviewed,
  requiredSetupComplete,
  setupProgress,
} from './setupChecklist'

describe('setupChecklist', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('detects profile readiness', () => {
    expect(isProfileReady(null)).toBe(false)
    expect(isProfileReady({ summary: '  ' })).toBe(false)
    expect(isProfileReady({ summary: 'Experienced professional' })).toBe(true)
    expect(isProfileReady({ experience_details: [{ position: 'Analyst' }] })).toBe(true)
  })

  it('detects search readiness', () => {
    expect(isSearchReady(undefined)).toBe(false)
    expect(isSearchReady({ positions: [] })).toBe(false)
    expect(isSearchReady({ positions: ['Designer'], search_targets: [{ remote: true }] })).toBe(true)
    expect(isSearchReady({ positions: ['Designer'], locations: ['Melbourne'] })).toBe(true)
  })

  it('detects platform session readiness', () => {
    expect(isPlatformReady({ linkedInSession: false, seekSession: false })).toBe(false)
    expect(isPlatformReady({ linkedInSession: true, seekSession: false })).toBe(true)
  })

  it('marks plan step complete after review or when unlimited', () => {
    localStorage.removeItem(SETUP_PLAN_REVIEW_KEY)
    expect(isPlanReviewReady({})).toBe(false)
    expect(isPlanReviewReady({ quotaUnlimited: true })).toBe(true)
    markPlanReviewed()
    expect(isPlanReviewReady({})).toBe(true)
    localStorage.removeItem(SETUP_PLAN_REVIEW_KEY)
  })

  it('computes required progress', () => {
    const steps = evaluateSetupSteps({
      profile: { summary: 'ok' },
      preferences: { positions: ['Role'], search_targets: [{ location: 'Berlin' }] },
      linkedInSession: false,
      seekSession: true,
    })
    expect(requiredSetupComplete(steps)).toBe(true)
    expect(setupProgress(steps)).toEqual({ done: 3, total: 3 })
    const planStep = steps.find(s => s.id === 'plan')
    expect(planStep?.required).toBe(false)
    expect(planStep?.to).toBe('/settings/plan')
  })
})
