// Typed API client: JSON requests against the backend, CSRF token handling,
// normalized errors, and an auth-expiry event for session redirects.

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export const CSRF_HEADER = 'X-CSRF-Token'

// Fired on window whenever the server answers 401 — the auth provider listens
// and drops the cached user so protected routes redirect to sign-in.
export const AUTH_EXPIRED_EVENT = 'budgetflow:auth-expired'

let csrfToken: string | null = null

export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

export function getCsrfToken(): string | null {
  return csrfToken
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = {}
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (method !== 'GET' && csrfToken) {
    headers[CSRF_HEADER] = csrfToken
  }

  const res = await fetch(`/api${path}`, {
    method,
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    credentials: 'same-origin',
    signal: options.signal,
  })

  let data: unknown = null
  if (res.status !== 204) {
    data = await res.json().catch(() => null)
  }

  if (!res.ok) {
    if (res.status === 401) {
      setCsrfToken(null)
      window.dispatchEvent(new Event(AUTH_EXPIRED_EVENT))
    }
    const message =
      data !== null &&
      typeof data === 'object' &&
      'error' in data &&
      typeof (data as { error: unknown }).error === 'string'
        ? (data as { error: string }).error
        : `Request failed (${res.status})`
    throw new ApiError(res.status, message)
  }

  return data as T
}

export const get = <T>(path: string, signal?: AbortSignal) => api<T>(path, { signal })

export const post = <T>(path: string, body?: unknown) => api<T>(path, { method: 'POST', body })

export const put = <T>(path: string, body?: unknown) => api<T>(path, { method: 'PUT', body })

export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' })
