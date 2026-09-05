import type { AxeMatchers } from 'vitest-axe'

// Registers toHaveNoViolations on expect(); the runtime half is
// expect.extend() in ./axe.ts. A declaration file so skipLibCheck tolerates
// the type-parameter drift between vitest 5 and matcher packages, exactly as
// it does for @testing-library/jest-dom's own vitest augmentation.
declare module 'vitest' {
  /* eslint-disable @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars, @typescript-eslint/no-explicit-any */
  interface Assertion<T = any> extends AxeMatchers {}
  interface AsymmetricMatchersContaining extends AxeMatchers {}
  /* eslint-enable @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars, @typescript-eslint/no-explicit-any */
}
