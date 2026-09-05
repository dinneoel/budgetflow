import '@testing-library/jest-dom/vitest'
import { afterEach, beforeEach, vi } from 'vitest'
import { setCsrfToken } from '../api/client'

// jsdom has no matchMedia; default to desktop unless a test overrides it via
// mockViewport from test/utils.
export function stubMatchMedia(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string): MediaQueryList => {
      return {
        matches,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      } as unknown as MediaQueryList
    }),
  )
}

beforeEach(() => {
  stubMatchMedia(true)
})

afterEach(() => {
  setCsrfToken(null)
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
