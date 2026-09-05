import { del, get, post, put } from './client'

export const goalTypes = ['savings', 'payoff', 'purchase'] as const
export type GoalType = (typeof goalTypes)[number]

export const goalTypeLabels: Record<GoalType, string> = {
  savings: 'Savings',
  payoff: 'Debt payoff',
  purchase: 'Purchase',
}

export interface Goal {
  id: string
  name: string
  type: GoalType
  targetAmount: number
  targetDate: string | null
  categoryId: string | null
  accountId: string | null
  archived: boolean
  currentBalance: number
  amountRemaining: number
  monthsRemaining: number
  requiredMonthlyContribution: number
  behindSchedule: boolean
  createdAt: string
  updatedAt: string
}

export interface GoalContribution {
  id: string
  goalId: string
  transactionId: string | null
  amount: number
  contributedOn: string
  notes: string
  createdAt: string
}

export interface GoalWithContributions extends Goal {
  contributions: GoalContribution[]
}

export interface GoalInput {
  name: string
  type: GoalType
  targetAmount: number
  targetDate: string | null
  categoryId: string | null
  accountId: string | null
}

export interface ContributionInput {
  amount: number
  date: string
  transactionId?: string | null
  notes: string
}

export async function listGoals(signal?: AbortSignal): Promise<Goal[]> {
  const res = await get<{ goals: Goal[] }>('/goals', signal)
  return res.goals
}

export async function getGoal(id: string, signal?: AbortSignal): Promise<GoalWithContributions> {
  const res = await get<{ goal: GoalWithContributions }>(`/goals/${id}`, signal)
  return res.goal
}

export async function createGoal(input: GoalInput): Promise<Goal> {
  const res = await post<{ goal: Goal }>('/goals', input)
  return res.goal
}

export async function updateGoal(id: string, input: GoalInput): Promise<Goal> {
  const res = await put<{ goal: Goal }>(`/goals/${id}`, input)
  return res.goal
}

export async function archiveGoal(id: string, archived: boolean): Promise<Goal> {
  const res = await post<{ goal: Goal }>(`/goals/${id}/${archived ? 'archive' : 'unarchive'}`)
  return res.goal
}

export async function addGoalContribution(
  goalId: string,
  input: ContributionInput,
): Promise<{ contribution: GoalContribution; goal: Goal }> {
  return post(`/goals/${goalId}/contributions`, input)
}

export async function deleteGoalContribution(
  goalId: string,
  contributionId: string,
): Promise<Goal> {
  const res = await del<{ goal: Goal }>(`/goals/${goalId}/contributions/${contributionId}`)
  return res.goal
}
