import { expect } from 'vitest'
import { configureAxe } from 'vitest-axe'
import * as axeMatchers from 'vitest-axe/matchers'

// The matcher's type augmentation lives in ./vitest-axe.d.ts.
expect.extend(axeMatchers)

// jsdom does not lay out or paint, so the contrast rule cannot run here.
// Screens render without the app shell in component tests, so the landmark
// rules would only flag the missing <main> wrapper, not the screen itself.
export const runAxe = configureAxe({
  rules: {
    'color-contrast': { enabled: false },
    region: { enabled: false },
  },
})
