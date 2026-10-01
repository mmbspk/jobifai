import { describe, expect, it } from 'vitest'
import { postAuthPathFromLocation, sanitizePostAuthPath } from './postAuthRedirect'

describe('postAuthRedirect', () => {
  it('defaults to dashboard home', () => {
    expect(postAuthPathFromLocation(undefined)).toBe('/')
  })

  it('preserves intended in-app path', () => {
    expect(postAuthPathFromLocation({ pathname: '/review', search: '?x=1' })).toBe('/review?x=1')
  })

  it('blocks auth and marketing paths', () => {
    expect(sanitizePostAuthPath('/login')).toBeNull()
    expect(sanitizePostAuthPath('//evil.test/review')).toBeNull()
  })
})
