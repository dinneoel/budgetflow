import { get, post, put } from './client'

export const budgetStatuses = ['on_track', 'approaching_limit', 'over_budget', 'unfunded'] as const
export type BudgetStatus = (typeof budgetStatuses)[number]

export interface BudgetPeriodSummary {
  id: string
  year: number
  month: number
  currency: string
  plannedIncome: number
  notes: string
  createdAt: string
  updatedAt: string
}

// One category's allocation with the server-computed budget math.
export interface AllocationDetail {
  categoryId: string
  amount: number
  rollover: number
  spending: number
  reserved: number
  remaining: number
  status: BudgetStatus
}

export interface BudgetPeriodDetail extends BudgetPeriodSummary {
  unallocated: number
  categories: AllocationDetail[]
}

export interface CreatePeriodInput {
  year: number
  month: number
  copyPrior: boolean
  plannedIncome: number
  notes: string
}

export interface UpdatePeriodInput {
  plannedIncome: number
  notes: string
}

export async function listPeriods(signal?: AbortSignal): Promise<BudgetPeriodSummary[]> {
  const res = await get<{ periods: BudgetPeriodSummary[] }>('/budgets', signal)
  return res.periods
}

export async function getPeriod(
  year: number,
  month: number,
  signal?: AbortSignal,
): Promise<BudgetPeriodDetail> {
  const res = await get<{ period: BudgetPeriodDetail }>(`/budgets/month/${year}/${month}`, signal)
  return res.period
}

export async function createPeriod(input: CreatePeriodInput): Promise<BudgetPeriodDetail> {
  const res = await post<{ period: BudgetPeriodDetail }>('/budgets', input)
  return res.period
}

export async function updatePeriod(
  periodId: string,
  input: UpdatePeriodInput,
): Promise<BudgetPeriodDetail> {
  const res = await put<{ period: BudgetPeriodDetail }>(`/budgets/${periodId}`, input)
  return res.period
}

export async function setAllocation(
  periodId: string,
  categoryId: string,
  amount: number,
): Promise<BudgetPeriodDetail> {
  const res = await put<{ period: BudgetPeriodDetail }>(
    `/budgets/${periodId}/allocations/${categoryId}`,
    { amount },
  )
  return res.period
}
