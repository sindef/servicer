import { afterEach, describe, expect, it, vi } from 'vitest'
import { authConfig, authError, authSession, authState, initializeAuth } from './auth'

describe('initializeAuth', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    // @ts-expect-error test cleanup
    delete globalThis.document
    authConfig.value = null
    authSession.value = null
    authError.value = null
    authState.value = 'loading'
  })

  it('sets recoverable error state when auth config fetch fails', async () => {
    Object.defineProperty(globalThis, 'document', {
      value: { cookie: '' },
      configurable: true
    })
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/auth/config')) {
          return new Response('config backend unavailable', { status: 503 })
        }
        return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })
      })
    )

    await initializeAuth()

    expect(authState.value).toBe('error')
    expect(authSession.value).toBeNull()
    expect(authConfig.value).toBeNull()
    expect(authError.value).toContain('config backend unavailable')
  })
})
