import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { AUTH_EXPIRED_EVENT } from '../../api/client'
import { meQueryKey } from './useAuth'

// Drops the cached user when any API call comes back 401, so ProtectedRoute
// redirects to sign-in as soon as the session expires.
export function AuthExpiryListener() {
  const queryClient = useQueryClient()

  useEffect(() => {
    const onExpired = () => {
      queryClient.setQueryData(meQueryKey, null)
    }
    window.addEventListener(AUTH_EXPIRED_EVENT, onExpired)
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, onExpired)
  }, [queryClient])

  return null
}
