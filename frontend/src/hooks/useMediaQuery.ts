import { useCallback, useSyncExternalStore } from 'react'

export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      const mql = window.matchMedia(query)
      mql.addEventListener('change', onStoreChange)
      return () => mql.removeEventListener('change', onStoreChange)
    },
    [query],
  )
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches)
}

// Breakpoint matching Tailwind's `md`; below it the shell switches to the
// mobile bottom navigation.
export function useIsDesktop(): boolean {
  return useMediaQuery('(min-width: 768px)')
}
