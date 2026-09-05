import { useState } from 'react'
import { Link } from 'react-router-dom'
import { notificationTypeLabels, type AppNotification } from '../../api/notifications'
import {
  useEvaluateNotifications,
  useMarkAllRead,
  useMarkRead,
  useNotifications,
  useUnreadCount,
} from './useNotifications'

function NotificationItem({
  notification,
  onNavigate,
}: {
  notification: AppNotification
  onNavigate: () => void
}) {
  const markRead = useMarkRead()

  const open = () => {
    if (!notification.read) {
      markRead.mutate(notification.id)
    }
    onNavigate()
  }

  return (
    <li
      className={[
        'border-b border-gray-100 px-4 py-3 text-sm last:border-b-0',
        notification.read ? 'bg-white' : 'bg-indigo-50',
      ].join(' ')}
    >
      <div className="flex items-start justify-between gap-2">
        <Link to={notification.actionUrl || '/'} onClick={open} className="min-w-0 flex-1">
          <p className="font-medium text-gray-900">
            {!notification.read ? (
              <span
                aria-hidden="true"
                className="mr-1.5 inline-block h-2 w-2 rounded-full bg-indigo-600 align-middle"
              />
            ) : null}
            {notification.title}
            {!notification.read ? <span className="sr-only"> (unread)</span> : null}
          </p>
          <p className="mt-0.5 text-gray-600">{notification.body}</p>
          <p className="mt-0.5 text-xs text-gray-500">
            {notificationTypeLabels[notification.type] ?? notification.type}
          </p>
        </Link>
        {!notification.read ? (
          <button
            type="button"
            onClick={() => markRead.mutate(notification.id)}
            className="shrink-0 text-xs font-medium text-indigo-600 hover:text-indigo-500"
          >
            Mark read
          </button>
        ) : null}
      </div>
    </li>
  )
}

// Bell button with an unread badge; opens a panel listing notifications with
// mark-read actions and links to the screen each warning is about.
export function NotificationCenter() {
  const [open, setOpen] = useState(false)
  const { data: unreadCount } = useUnreadCount()
  const { data: page, isLoading } = useNotifications(open)
  const evaluate = useEvaluateNotifications()
  const markAllRead = useMarkAllRead()

  const toggle = () => {
    const next = !open
    setOpen(next)
    if (next) {
      // Sweep time-based conditions (bills due, month missing, goals behind)
      // so the panel is current the moment it opens.
      evaluate.mutate()
    }
  }

  const badge = unreadCount ?? 0

  return (
    <div className="relative">
      <button
        type="button"
        aria-label={badge > 0 ? `Notifications (${badge} unread)` : 'Notifications'}
        aria-expanded={open}
        onClick={toggle}
        className="relative rounded-md p-2 text-gray-600 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500"
      >
        <span aria-hidden="true" className="text-lg leading-none">
          🔔
        </span>
        {badge > 0 ? (
          <span
            aria-hidden="true"
            className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-600 px-1 text-[10px] font-bold text-white"
          >
            {badge > 99 ? '99+' : badge}
          </span>
        ) : null}
      </button>

      {open ? (
        <section
          aria-label="Notifications"
          className="absolute left-0 z-30 mt-2 max-h-96 w-80 overflow-y-auto rounded-lg border border-gray-200 bg-white shadow-lg max-md:fixed max-md:inset-x-2 max-md:top-14 max-md:w-auto"
        >
          <div className="flex items-center justify-between border-b border-gray-200 px-4 py-2">
            <h2 className="text-sm font-semibold text-gray-900">Notifications</h2>
            {(page?.notifications ?? []).some((n) => !n.read) ? (
              <button
                type="button"
                onClick={() => markAllRead.mutate()}
                className="text-xs font-medium text-indigo-600 hover:text-indigo-500"
              >
                Mark all read
              </button>
            ) : null}
          </div>
          {isLoading ? <p className="px-4 py-3 text-sm text-gray-600">Loading…</p> : null}
          {!isLoading && (page?.notifications ?? []).length === 0 ? (
            <p className="px-4 py-3 text-sm text-gray-600">
              You're all caught up — nothing needs attention.
            </p>
          ) : null}
          <ul>
            {(page?.notifications ?? []).map((n) => (
              <NotificationItem key={n.id} notification={n} onNavigate={() => setOpen(false)} />
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}
