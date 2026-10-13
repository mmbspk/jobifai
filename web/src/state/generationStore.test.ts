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

    // Change inputs — use a short description so base mode (mockGenerate) is still used.
    // The fingerprint changes because jobDesc differs; a mode change is not required.
    generationStore.setForm({ jobDesc: 'Different role.' })

    mockGenerate.mockClear()
    resolveRef.current = null
    rejectRef.current = null

    startResume()
    await flushMicrotasks()

    const secondKey = getPendingKey('resume')
    expect(secondKey).toBeDefined()
    expect(secondKey).not.toBe(firstKey)

    generationStore.cancelGenerate('resume')
    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
    await flushMicrotasks()
  })

})

// ─── Material-input fingerprint coverage ──────────────────────────────────────
//
// Each test confirms that changing a specific input generates a fresh key,
// verifying the expanded computeFingerprint covers ALL billing-relevant fields.
// All tests use base mode (empty or short form) so that mockGenerate (not
// generateTailored) is called — allowing the test to control the promise lifecycle
// via rejectRef.current.

describe('computeFingerprint covers all material inputs', () => {

  // Start, capture pending key, abort, and return the key.
  async function captureKeyAfterAbort(skipUrlFetch = true): Promise<string> {
    startResume(skipUrlFetch)
    await flushMicrotasks()
    const key = getPendingKey('resume')!
    generationStore.cancelGenerate('resume')
    rejectRef.current!(new DOMException('AbortError', 'AbortError'))
    await flushMicrotasks()
    await flushMicrotasks()
    return key
  }

  beforeEach(() => {
    resetStore()
    mockGenerate.mockClear()
    resolveRef.current = null
    rejectRef.current = null
  })

  it('same inputs → same key on retry', async () => {
    const k1 = await captureKeyAfterAbort()
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).toBe(k1)
  })

  it('changed jobDesc → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    // Keep desc short (≤20 chars) so base mode is used for the second attempt too.
    generationStore.setForm({ jobDesc: 'short desc' })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('changed market → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    generationStore.setForm({ market: 'de' })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('changed promptHint → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    generationStore.setForm({ promptHint: 'Emphasise leadership.' })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('changed linkedinUrl → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    generationStore.setForm({ linkedinUrl: 'https://linkedin.com/in/alex' })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('changed githubUrl → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    generationStore.setForm({ githubUrl: 'https://github.com/alex' })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('changed skipUrlFetch → new key (skipUrlFetch is part of fingerprint)', async () => {
    // With empty form both skipUrlFetch=true and skipUrlFetch=false use base mode
    // (no desc → no tailored), but the fingerprint includes the skipUrlFetch value.
    const k1 = await captureKeyAfterAbort(true)
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort(false)
    expect(k2).not.toBe(k1)
  })

  it('changed resumeFile identity → new key', async () => {
    const k1 = await captureKeyAfterAbort()
    // Simulate attaching a file (only identity matters — name/size/type/lastModified).
    const fakeFile = { name: 'cv.pdf', size: 12345, type: 'application/pdf', lastModified: 1700000000000 } as File
    generationStore.setForm({ resumeFile: fakeFile })
    mockGenerate.mockClear(); resolveRef.current = null; rejectRef.current = null
    const k2 = await captureKeyAfterAbort()
    expect(k2).not.toBe(k1)
  })

  it('success → key cleared; evaluate/questions tabs never get pending keys', async () => {
    startResume()
    await flushMicrotasks()
    resolveRef.current!(new Blob(['%PDF'], { type: 'application/pdf' }))
    await flushMicrotasks()
    await flushMicrotasks()
    expect(getPendingKey('resume')).toBeUndefined()

    // evaluate and questions tabs must never have a pending key stored.
    expect(getPendingKey('evaluate')).toBeUndefined()
    expect(getPendingKey('questions')).toBeUndefined()
  })

})
