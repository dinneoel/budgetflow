import { describe, expect, it, vi } from 'vitest'
import { AUTH_EXPIRED_EVENT, ApiError, api, getCsrfToken, post, setCsrfToken } from './client'
import { jsonResponse, mockFetch } from '../test/utils'

describe('api client', () => {
  it('prefixes paths with /api and parses JSON', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, { status: 'ok' }))

    const result = await api<{ status: string }>('/health')

    expect(result).toEqual({ status: 'ok' })
    expect(fetchMock).toHaveBeenCalledWith('/api/health', expect.objectContaining({ method: 'GET' }))
  })

  it('sends the CSRF token on mutating requests but not on GET', async () => {
    setCsrfToken('csrf-123')
    const fetchMock = mockFetch(() => jsonResponse(200, {}))

    await post('/auth/sign-out')
    await api('/auth/me')

    const postInit = fetchMock.mock.calls[0][1] as RequestInit
    const getInit = fetchMock.mock.calls[1][1] as RequestInit
    expect((postInit.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-123')
    expect((getInit.headers as Record<string, string>)['X-CSRF-Token']).toBeUndefined()
  })

  it('serializes the body and sets the content type', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, {}))

    await post('/auth/sign-in', { email: 'a@b.co', password: 'pw' })

    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.body).toBe(JSON.stringify({ email: 'a@b.co', password: 'pw' }))
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('normalizes server errors into ApiError with the server message', async () => {
    mockFetch(() => jsonResponse(409, { error: 'email already in use' }))

    const err = await api('/auth/sign-up', { method: 'POST', body: {} }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(409)
    expect((err as ApiError).message).toBe('email already in use')
  })

  it('falls back to a generic message when the error body is not JSON', async () => {
    mockFetch(() => new Response('gateway exploded', { status: 502 }))

    const err = await api('/health').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).message).toBe('Request failed (502)')
  })

  it('returns undefined for 204 responses', async () => {
    mockFetch(() => new Response(null, { status: 204 }))

    await expect(post('/auth/sign-out')).resolves.toBeNull()
  })

  it('dispatches the auth-expired event and clears the CSRF token on 401', async () => {
    setCsrfToken('stale-token')
    mockFetch(() => jsonResponse(401, { error: 'unauthenticated' }))
    const listener = vi.fn()
    window.addEventListener(AUTH_EXPIRED_EVENT, listener)

    await expect(api('/accounts')).rejects.toMatchObject({ status: 401 })

    expect(listener).toHaveBeenCalledOnce()
    expect(getCsrfToken()).toBeNull()
    window.removeEventListener(AUTH_EXPIRED_EVENT, listener)
  })
})
