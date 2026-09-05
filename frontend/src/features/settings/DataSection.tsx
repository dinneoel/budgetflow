import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { deleteAccount, fullExportUrl } from '../../api/users'

// Export download plus the delete-account flow. Deletion is gated behind an
// explicit confirmation checkbox and password re-entry before the request
// can be sent.
export function DataSection() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const [password, setPassword] = useState('')
  const [acknowledged, setAcknowledged] = useState(false)

  const destroy = useMutation({
    mutationFn: (pw: string) => deleteAccount(pw),
    onSuccess: () => {
      queryClient.clear()
      navigate('/sign-in')
    },
  })

  const canDelete = acknowledged && password.length > 0 && !destroy.isPending

  return (
    <section aria-label="Your data" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
      <h3 className="text-base font-semibold text-gray-900">Your data</h3>
      <p className="mt-1 text-sm text-gray-600">
        Download everything you have stored in BudgetFlow as a ZIP of CSV files.
      </p>
      <a
        href={fullExportUrl}
        download
        className="mt-3 inline-block rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
      >
        Export all data
      </a>

      <div className="mt-6 border-t border-gray-200 pt-4">
        <h4 className="text-sm font-semibold text-red-700">Delete account</h4>
        <p className="mt-1 text-sm text-gray-600">
          Permanently deletes your account and all data — accounts, transactions, budgets, goals,
          and settings. This cannot be undone. Export your data first if you want a copy.
        </p>
        {!confirming ? (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            className="mt-3 rounded-md border border-red-300 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-50"
          >
            Delete account…
          </button>
        ) : (
          <form
            className="mt-3 space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              if (canDelete) destroy.mutate(password)
            }}
          >
            <label className="flex items-start gap-2 text-sm text-gray-700">
              <input
                type="checkbox"
                className="mt-0.5 h-4 w-4 rounded"
                checked={acknowledged}
                onChange={(e) => setAcknowledged(e.target.checked)}
              />
              I understand this permanently deletes all my data
            </label>
            <div>
              <label htmlFor="delete-password" className="block text-sm font-medium text-gray-700">
                Confirm your password
              </label>
              <input
                id="delete-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
              />
            </div>
            {destroy.error ? (
              <p role="alert" className="text-sm text-red-600">
                {destroy.error.message}
              </p>
            ) : null}
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={!canDelete}
                className="rounded-md bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-50"
              >
                {destroy.isPending ? 'Deleting…' : 'Permanently delete my account'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setConfirming(false)
                  setPassword('')
                  setAcknowledged(false)
                }}
                className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
              >
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>
    </section>
  )
}
