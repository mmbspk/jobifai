import { describe, expect, test } from 'vitest'
import { quotaBlockMessage } from './quotaMessages'

describe('quotaBlockMessage', () => {
  test('trial expired', () => {
    expect(quotaBlockMessage('trial_expired')).toMatch(/trial period has ended/i)
  })

  test('period exhausted', () => {
    expect(quotaBlockMessage('period_exhausted', 'starter')).toMatch(/Monthly credits/)
  })
})
