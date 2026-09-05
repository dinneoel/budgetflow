import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

interface EmptyStateProps {
  title: string
  description: string
  actionLabel?: string
  actionTo?: string
  children?: ReactNode
}

// First-time-use placeholder for a primary area: what this screen will show
// and the next step to get there.
export function EmptyState({ title, description, actionLabel, actionTo, children }: EmptyStateProps) {
  return (
    <div className="mx-auto mt-12 max-w-md rounded-lg border border-dashed border-gray-300 bg-white p-8 text-center">
      <h2 className="text-lg font-semibold text-gray-900">{title}</h2>
      <p className="mt-2 text-sm text-gray-600">{description}</p>
      {actionLabel && actionTo ? (
        <Link
          to={actionTo}
          className="mt-4 inline-block rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
        >
          {actionLabel}
        </Link>
      ) : null}
      {children}
    </div>
  )
}
