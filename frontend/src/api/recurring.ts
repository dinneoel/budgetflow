import { get, post, put } from './client'

export const frequencies = ['weekly', 'monthly', 'annual', 'custom'] as const
export type Frequency = (typeof frequencies)[number]

export const frequencyLabels: Record<Frequency, string> = {
  weekly: 'Weekly',
  monthly: 'Monthly',
  annual: 'Annual',
  custom: 'Custom interval',
}

export interface RecurringRule {
  id: string
  name: string
  accountId: string
  categoryId: string
  amount: number
  frequency: Frequency
  customIntervalDays: number | null
  nextDueDate: string
  reminderLeadDays: number
  archived: boolean
  createdAt: string
  updatedAt: string
}

export interface RecurringRuleInput {
  name: string
  accountId: string
  categoryId: string
  amount: number
  frequency: Frequency
  customIntervalDays: number | null
  nextDueDate: string
  reminderLeadDays: number
}

export interface MarkPaidInput {
  date?: string
  amount?: number
  status?: string
}

export interface PaidResult {
  rule: RecurringRule
  transaction: {
    id: string
    accountId: string
    categoryId: string | null
    type: string
    status: string
    amount: number
    date: string
    payee: string
    recurringRuleId: string | null
  }
}

export interface UpcomingBill extends RecurringRule {
  daysUntilDue: number
}

export async function listRecurringRules(signal?: AbortSignal): Promise<RecurringRule[]> {
  const res = await get<{ rules: RecurringRule[] }>('/recurring', signal)
  return res.rules
}

export async function createRecurringRule(input: RecurringRuleInput): Promise<RecurringRule> {
  const res = await post<{ rule: RecurringRule }>('/recurring', input)
  return res.rule
}

export async function updateRecurringRule(
  id: string,
  input: RecurringRuleInput,
): Promise<RecurringRule> {
  const res = await put<{ rule: RecurringRule }>(`/recurring/${id}`, input)
  return res.rule
}

export async function archiveRecurringRule(id: string, archived: boolean): Promise<RecurringRule> {
  const res = await post<{ rule: RecurringRule }>(
    `/recurring/${id}/${archived ? 'archive' : 'unarchive'}`,
  )
  return res.rule
}

export async function markRecurringRulePaid(
  id: string,
  input: MarkPaidInput = {},
): Promise<PaidResult> {
  return post<PaidResult>(`/recurring/${id}/pay`, input)
}

export async function matchRecurringRule(
  id: string,
  transactionId: string,
): Promise<PaidResult> {
  return post<PaidResult>(`/recurring/${id}/match`, { transactionId })
}

export async function listUpcomingBills(
  days: number,
  signal?: AbortSignal,
): Promise<UpcomingBill[]> {
  const res = await get<{ upcoming: UpcomingBill[] }>(`/recurring/upcoming?days=${days}`, signal)
  return res.upcoming
}
