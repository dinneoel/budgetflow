import { get, put } from './client'

export const notificationTypes = [
  'category_threshold',
  'category_over_budget',
  'bill_due',
  'budget_month_missing',
  'goal_behind_schedule',
  'import_needs_review',
] as const

export type NotificationType = (typeof notificationTypes)[number]

export const notificationTypeLabels: Record<NotificationType, string> = {
  category_threshold: 'Category approaching its limit',
  category_over_budget: 'Category over budget',
  bill_due: 'Bill due soon',
  budget_month_missing: 'Budget month not created',
  goal_behind_schedule: 'Goal behind schedule',
  import_needs_review: 'Import needs review',
}

export interface NotificationPreference {
  type: NotificationType
  enabled: boolean
  thresholdPct: number | null
}

export async function listNotificationPreferences(
  signal?: AbortSignal,
): Promise<NotificationPreference[]> {
  const res = await get<{ preferences: NotificationPreference[] }>(
    '/notifications/preferences',
    signal,
  )
  return res.preferences
}

export async function setNotificationPreference(
  pref: NotificationPreference,
): Promise<NotificationPreference> {
  const res = await put<{ preference: NotificationPreference }>('/notifications/preferences', pref)
  return res.preference
}
