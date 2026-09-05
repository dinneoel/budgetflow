import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi } from 'vitest'
import type { User } from '../api/auth'
import { stubMatchMedia } from './setup'

export function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

export function renderWithProviders(
  ui: ReactElement,
  { route = '/', queryClient = makeQueryClient() } = {},
) {
  const result = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...result, queryClient }
}

export const testUser: User = {
  id: '3c6a63dd-6b0c-4b7a-9b8e-0a1a2b3c4d5e',
  email: 'leonid@example.com',
  name: 'Leonid',
  locale: 'en-US',
  timeZone: 'Europe/Kyiv',
  firstDayOfWeek: 1,
  defaultCurrency: 'UAH',
  createdAt: '2026-09-01T00:00:00Z',
}

export function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  })
}

// Installs a fetch mock; returns the mock for call assertions.
export function mockFetch(handler: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    return handler(url, init)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

export function mockViewport(desktop: boolean) {
  stubMatchMedia(desktop)
}
