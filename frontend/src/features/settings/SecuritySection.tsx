import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { listSessions } from '../../api/auth'
import { useSignOutAll } from '../auth/useAuth'

export function SecuritySection() {
  const navigate = useNavigate()
  const { data: sessions, isLoading, error } = useQuery({
    queryKey: ['auth', 'sessions'],
    queryFn: ({ signal }) => listSessions(signal),
  })
  const signOutAll = useSignOutAll()

  return (
    <section aria-label="Security" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
      <h3 className="text-base font-semibold text-gray-900">Security</h3>
      <p className="mt-1 text-sm text-gray-600">Devices currently signed in to your account.</p>
      {isLoading ? <p className="mt-3 text-sm text-gray-600">Loading…</p> : null}
      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {error.message}
        </p>
      ) : null}
      <ul className="mt-3 divide-y divide-gray-100">
        {(sessions ?? []).map((s) => (
          <li key={s.id} className="py-2 text-sm">
            <p className="font-medium text-gray-900">
              {s.userAgent || 'Unknown device'}
              {s.current ? (
                <span className="ml-2 rounded bg-green-100 px-1.5 py-0.5 text-xs text-green-700">
                  This device
                </span>
              ) : null}
            </p>
            <p className="text-gray-600">
              {s.ipAddress} · signed in {new Date(s.createdAt).toLocaleString()}
            </p>
          </li>
        ))}
      </ul>
      <button
        type="button"
        disabled={signOutAll.isPending}
        onClick={() =>
          signOutAll.mutate(undefined, { onSettled: () => navigate('/sign-in') })
        }
        className="mt-4 rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
      >
        Sign out of all devices
      </button>
    </section>
  )
}
