import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useCurrentUser } from './useAuth'

// Gate for authenticated areas: waits for the session check, then either
// renders the child routes or redirects to sign-in (remembering the origin).
export function ProtectedRoute() {
  const { data: user, isPending } = useCurrentUser()
  const location = useLocation()

  if (isPending) {
    return (
      <div className="flex min-h-screen items-center justify-center" role="status">
        <p className="text-sm text-gray-500">Loading…</p>
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/sign-in" state={{ from: location }} replace />
  }

  return <Outlet />
}
