/**
 * Frontend idempotency regression tests for generationStore.
 *
 * These tests verify the billing key lifecycle:
 *   - First attempt → fresh key generated and stored in pendingKeys
 *   - Uncertain failure (timeout/abort) → key retained for retry
 *   - Retry with same inputs → same key reused (no double-bill)
 *   - Confirmed success → key cleared (next attempt is a new action)
 *   - Changed material inputs → new key generated
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

// vi.hoisted ensures mockGenerate and the shared resolve/reject refs are initialised
// before vi.mock's factory runs (vi.mock is hoisted to file top by vitest).
const { mockGenerate, resolveRef, rejectRef } = vi.hoisted(() => {
  const resolveRef = { current: null as ((v: Blob) => void) | null }
  const rejectRef = { current: null as ((r: unknown) => void) | null }
  const mockGenerate = vi.fn(
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    (..._args: unknown[]) =>
      new Promise<Blob>((resolve, reject) => {
        resolveRef.current = resolve
        rejectRef.current = reject
      })
  )
  return { mockGenerate, resolveRef, rejectRef }
})

vi.mock('../api/resume', () => ({
  resumeApi: {
    generate: mockGenerate,
    generateTailored: vi.fn(() => new Promise(() => {})),
    generateCoverLetter: vi.fn(() => new Promise(() => {})),
    answerQuestions: vi.fn(() => new Promise(() => {})),
    evaluate: vi.fn(() => new Promise(() => {})),
    checkHalal: vi.fn(() => new Promise(() => {})),
  },
}))

// jsdom does not provide URL.createObjectURL — stub it.
globalThis.URL.createObjectURL = vi.fn(() => 'blob:stub-url')
globalThis.URL.revokeObjectURL = vi.fn()

import { generationStore, clearPendingKeys, getPendingKey } from './generationStore'

// ─── Helpers ─────────────────────────────────────────────────────────────────

const BLANK_FORM = {
  jobUrl: '',
  jobDesc: '',
  market: '',
  promptHint: '',
  linkedinUrl: '',
  githubUrl: '',
  resumeFile: null,
  questions: [{ id: 0, value: '' }],
}

function resetStore() {
  generationStore.setForm(BLANK_FORM)
  generationStore.cancelGenerate('resume')
  clearPendingKeys()
}

function startResume(skipUrlFetch = true) {
  return generationStore.startGenerate('resume', skipUrlFetch, false)
}

// Flush microtasks so pending promise continuations run before asserting.
function flushMicrotasks() {
  return new Promise<void>(resolve => setTimeout(resolve, 0))
}

// ─── Tests ───────────────────────────────────────────────────────────────────

beforeEach(() => {
  resetStore()
  mockGenerate.mockClear()
  resolveRef.current = null
  rejectRef.current = null
})

describe('generationStore idempotency key lifecycle', () => {

  it('first attempt → generates and stores a key', async () => {
    startResume()
    await flushMicrotasks()

    const key = getPendingKey('resume')
    expect(key).toBeDefined()
    expect(typeof key).toBe('string')
    expect(key!.length).toBeGreaterThan(0)

    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
  })

  it('uncertain failure (abort) → key retained for retry', async () => {
    startResume()
    await flushMicrotasks()

    const keyBefore = getPendingKey('resume')
    expect(keyBefore).toBeDefined()

    generationStore.cancelGenerate('resume')
    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
    await flushMicrotasks()

    expect(getPendingKey('resume')).toBe(keyBefore)
  })

  it('retry with same inputs → reuses existing key', async () => {
    // First attempt — aborted (outcome uncertain).
    startResume()
    await flushMicrotasks()
    const firstKey = getPendingKey('resume')

    generationStore.cancelGenerate('resume')
    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
    await flushMicrotasks()

    // Second attempt — same form, no inputs changed.
    mockGenerate.mockClear()
    resolveRef.current = null
    rejectRef.current = null

    startResume()
    await flushMicrotasks()

    expect(getPendingKey('resume')).toBe(firstKey)
    // Verify the key passed to the API is the same retained key.
    expect(mockGenerate).toHaveBeenCalledTimes(1)
    const passedKey = mockGenerate.mock.calls[0][6] // 7th arg is idempotencyKey
    expect(passedKey).toBe(firstKey)

    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
  })

  it('success → key cleared; next attempt gets a fresh key', async () => {
    startResume()
    await flushMicrotasks()
    const keyBefore = getPendingKey('resume')
    expect(keyBefore).toBeDefined()

    resolveRef.current!(new Blob(['%PDF'], { type: 'application/pdf' }))
    await flushMicrotasks()
    await flushMicrotasks()

    expect(getPendingKey('resume')).toBeUndefined()

    // Next attempt must produce a distinct fresh key.
    mockGenerate.mockClear()
    resolveRef.current = null
    rejectRef.current = null

    startResume()
    await flushMicrotasks()

    const newKey = getPendingKey('resume')
    expect(newKey).toBeDefined()
    expect(newKey).not.toBe(keyBefore)

    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
  })

  it('materially changed inputs → new key generated', async () => {
    // First attempt (no job input → base render path).
    startResume()
    await flushMicrotasks()
    const firstKey = getPendingKey('resume')

    generationStore.cancelGenerate('resume')
    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
    await flushMicrotasks()

    // Change inputs — triggers a new fingerprint.
    generationStore.setForm({ jobDesc: 'Senior operations manager needed for logistics team.' })

    mockGenerate.mockClear()
    resolveRef.current = null
    rejectRef.current = null

    startResume(false)
    await flushMicrotasks()

    const secondKey = getPendingKey('resume')
    expect(secondKey).toBeDefined()
    expect(secondKey).not.toBe(firstKey)

    // cancelGenerate aborts the in-flight request regardless of which API path was taken.
    generationStore.cancelGenerate('resume')
    await flushMicrotasks()
  })

})
