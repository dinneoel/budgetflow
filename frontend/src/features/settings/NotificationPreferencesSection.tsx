import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  listNotificationPreferences,
  notificationTypeLabels,
  setNotificationPreference,
  type NotificationPreference,
} from '../../api/notifications'

const preferencesQueryKey = ['notifications', 'preferences'] as const

function ThresholdEditor({
  pref,
  onSave,
  pending,
}: {
  pref: NotificationPreference
  onSave: (thresholdPct: number) => void
  pending: boolean
}) {
  const [value, setValue] = useState(String(pref.thresholdPct ?? 80))
  const parsed = Number(value)
  const valid = Number.isInteger(parsed) && parsed >= 1 && parsed <= 100

  return (
    <form
      className="mt-1 flex items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (valid) onSave(parsed)
      }}
    >
      <label htmlFor="threshold-pct" className="text-sm text-gray-600">
        Warn at
      </label>
      <input
        id="threshold-pct"
        type="number"
        min={1}
        max={100}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-invalid={valid ? undefined : true}
        className="w-20 rounded-md border border-gray-300 px-2 py-1 text-sm"
      />
      <span className="text-sm text-gray-600">% of budget</span>
      <button
        type="submit"
        disabled={!valid || pending}
        className="rounded-md border border-gray-300 px-2 py-1 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
      >
        Save
      </button>
    </form>
  )
}

export function NotificationPreferencesSection() {
  const queryClient = useQueryClient()
  const { data: prefs, isLoading, error } = useQuery({
    queryKey: preferencesQueryKey,
    queryFn: ({ signal }) => listNotificationPreferences(signal),
  })
  const save = useMutation({
    mutationFn: setNotificationPreference,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: preferencesQueryKey }),
  })

  return (
    <section aria-label="Notification preferences" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
      <h3 className="text-base font-semibold text-gray-900">Notifications</h3>
      <p className="mt-1 text-sm text-gray-600">
        Choose which in-app alerts BudgetFlow shows you.
      </p>
      {isLoading ? <p className="mt-3 text-sm text-gray-600">Loading…</p> : null}
      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {error.message}
        </p>
      ) : null}
      {save.error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {save.error.message}
        </p>
      ) : null}
      <ul className="mt-3 space-y-3">
        {(prefs ?? []).map((pref) => (
          <li key={pref.type}>
            <label className="flex items-center gap-2 text-sm text-gray-700">
              <input
                type="checkbox"
                className="h-4 w-4 rounded"
                checked={pref.enabled}
                disabled={save.isPending}
                onChange={(e) => save.mutate({ ...pref, enabled: e.target.checked })}
              />
              {notificationTypeLabels[pref.type]}
            </label>
            {pref.type === 'category_threshold' && pref.enabled ? (
              <ThresholdEditor
                pref={pref}
                pending={save.isPending}
                onSave={(thresholdPct) => save.mutate({ ...pref, thresholdPct })}
              />
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  )
}
