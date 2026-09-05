import { get } from './client'
import type { BudgetStatus } from './budgets'

// Budget health values for the dashboard indicator.
export const budgetHealths = ['no_budget', 'on_track', 'approaching_limit', 'over_budget'] as const
export type BudgetHealth = (typeof budgetHealths)[number]

export interface AccountBalance {
  accountId: string
  name: string
  type: string
  currency: string
  balance: number
}

export interface CategoryAtRisk {
  categoryId: string
  name: string
  budgeted: number
  rollover: number
  spending: number
  reserved: number
  remaining: number
  status: BudgetStatus
}

export interface UpcomingBill {
  ruleId: string
  name: string
  amount: number
  accountId: string
  categoryId: string | null
  dueDate: string
  daysUntilDue: number
}

export interface DashboardTransaction {
  id: string
  accountId: string
  categoryId: string | null
  type: string
  status: string
  amount: number
  date: string
  payee: string
  notes: string
}

export interface DashboardGoal {
  id: string
  name: string
  type: string
  targetAmount: number
  targetDate: string | null
  currentBalance: number
  amountRemaining: number
  requiredMonthlyContribution: number | null
  behindSchedule: boolean
}

export interface DashboardBudget {
  exists: boolean
  year: number
  month: number
  plannedIncome: number
  unallocated: number
  totalBudgeted: number
  totalSpending: number
  totalRemaining: number
  health: BudgetHealth
}

export interface Dashboard {
  currency: string
  availableBalance: number
  foreignBalances: AccountBalance[]
  mtdIncome: number
  mtdSpending: number
  budget: DashboardBudget
  categoriesAtRisk: CategoryAtRisk[]
  upcomingBills: UpcomingBill[]
  recentTransactions: DashboardTransaction[]
  goals: DashboardGoal[]
}

export const getDashboard = (signal?: AbortSignal) => get<Dashboard>('/dashboard', signal)

export interface CategorySpendingRow {
  categoryId: string
  name: string
  group: string
  spending: number
}

export interface SpendingByCategory {
  from: string
  to: string
  totalSpending: number
  categories: CategorySpendingRow[]
}

export function getSpendingByCategory(
  from: string | null,
  to: string | null,
  signal?: AbortSignal,
): Promise<SpendingByCategory> {
  return get<SpendingByCategory>(`/reports/spending-by-category${rangeQuery(from, to)}`, signal)
}

export interface TrendMonth {
  year: number
  month: number
  spending: number
}

export async function getMonthlyTrend(months: number, signal?: AbortSignal): Promise<TrendMonth[]> {
  const res = await get<{ months: TrendMonth[] }>(`/reports/monthly-trend?months=${months}`, signal)
  return res.months
}

export interface IncomeExpenseMonth {
  year: number
  month: number
  income: number
  expenses: number
  net: number
}

export async function getIncomeVsExpenses(
  months: number,
  signal?: AbortSignal,
): Promise<IncomeExpenseMonth[]> {
  const res = await get<{ months: IncomeExpenseMonth[] }>(
    `/reports/income-vs-expenses?months=${months}`,
    signal,
  )
  return res.months
}

export interface CashFlowMonth {
  year: number
  month: number
  inflow: number
  outflow: number
  net: number
}

export async function getCashFlow(months: number, signal?: AbortSignal): Promise<CashFlowMonth[]> {
  const res = await get<{ months: CashFlowMonth[] }>(`/reports/cash-flow?months=${months}`, signal)
  return res.months
}

export interface NetWorth {
  currency: string
  total: number
  accounts: AccountBalance[]
  foreignBalances: AccountBalance[]
  foreignTotals: Record<string, number>
}

export const getNetWorth = (signal?: AbortSignal) => get<NetWorth>('/reports/net-worth', signal)

export interface TopPayee {
  payee: string
  transactionCount: number
  spending: number
}

export interface TopPayees {
  from: string
  to: string
  payees: TopPayee[]
}

export function getTopPayees(
  from: string | null,
  to: string | null,
  signal?: AbortSignal,
): Promise<TopPayees> {
  return get<TopPayees>(`/reports/top-payees${rangeQuery(from, to)}`, signal)
}

function rangeQuery(from: string | null, to: string | null): string {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

// CSV download URLs (GET endpoints served as attachments; used as link hrefs).
export function transactionsCsvUrl(from: string | null, to: string | null): string {
  return `/api/export/transactions.csv${rangeQuery(from, to)}`
}

export function budgetCsvUrl(year: number, month: number): string {
  return `/api/export/budget.csv?year=${year}&month=${month}`
}

export const categoriesCsvUrl = '/api/export/categories.csv'
export const goalsCsvUrl = '/api/export/goals.csv'
