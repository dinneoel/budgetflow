import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  evaluateNotifications,
  getUnreadCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
} from '../../api/notifications'

export const notificationsQueryKey = ['notifications'] as const
export const unreadCountQueryKey = [...notificationsQueryKey, 'unread-count'] as const

export function useUnreadCount() {
  return useQuery({
    queryKey: unreadCountQueryKey,
    queryFn: ({ signal }) => getUnreadCount(signal),
    refetchInterval: 60_000,
  })
}

export function useNotifications(enabled: boolean) {
  return useQuery({
    queryKey: [...notificationsQueryKey, 'list'],
    queryFn: ({ signal }) => listNotifications(50, 0, signal),
    enabled,
  })
}

function useInvalidateNotifications() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: notificationsQueryKey })
}

// Server-side sweep for time-based conditions; run when the panel opens.
export function useEvaluateNotifications() {
  const invalidate = useInvalidateNotifications()
  return useMutation({
    mutationFn: evaluateNotifications,
    onSuccess: invalidate,
  })
}

export function useMarkRead() {
  const invalidate = useInvalidateNotifications()
  return useMutation({
    mutationFn: (id: string) => markNotificationRead(id),
    onSuccess: invalidate,
  })
}

export function useMarkAllRead() {
  const invalidate = useInvalidateNotifications()
  return useMutation({
    mutationFn: markAllNotificationsRead,
    onSuccess: invalidate,
  })
}
