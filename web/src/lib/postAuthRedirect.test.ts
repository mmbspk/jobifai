import { describe, expect, it } from 'vitest'
import {
  clearPostAuthRedirect,
  postAuthPathFromLocation,
  resolvePostAuthDestination,
  sanitizePostAuthPath,
  savePostAuthRedirect,
} from './postAuthRedirect'

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

  it('resolvePostAuthDestination falls back to session storage', () => {
    clearPostAuthRedirect()
    savePostAuthRedirect('/review')
    expect(resolvePostAuthDestination(undefined)).toBe('/review')
    clearPostAuthRedirect()
  })

  it('resolvePostAuthDestination prefers router state over session', () => {
    savePostAuthRedirect('/jobs/applied')
    expect(resolvePostAuthDestination({ pathname: '/review' })).toBe('/review')
    clearPostAuthRedirect()
  })
})
