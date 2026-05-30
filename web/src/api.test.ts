import { describe, expect, it } from 'vitest'
import { ApiError, parseApiError } from './api'

describe('parseApiError', () => {
  it('extracts JSON message and request id', async () => {
    const response = new Response(JSON.stringify({ error: 'validation failed', code: 'INVALID' }), {
      status: 400,
      headers: {
        'content-type': 'application/json',
        'x-request-id': 'req-1234'
      }
    })

    const err = await parseApiError(response)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.message).toContain('validation failed')
    expect(err.message).toContain('req-1234')
    expect(err.status).toBe(400)
    expect(err.code).toBe('INVALID')
  })

  it('falls back to status text when body is empty', async () => {
    const response = new Response('', { status: 500 })
    const err = await parseApiError(response)
    expect(err.message).toContain('Request failed (500)')
  })
})
