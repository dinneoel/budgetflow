import { useQuery } from '@tanstack/react-query'
import {
  getCashFlow,
  getIncomeVsExpenses,
  getMonthlyTrend,
  getNetWorth,
  getSpendingByCategory,
  getTopPayees,
} from '../../api/reports'

export function useSpendingByCategory(from: string | null, to: string | null) {
  return useQuery({
    queryKey: ['reports', 'spending-by-category', from, to],
    queryFn: ({ signal }) => getSpendingByCategory(from, to, signal),
  })
}

export function useMonthlyTrend(months: number) {
  return useQuery({
    queryKey: ['reports', 'monthly-trend', months],
    queryFn: ({ signal }) => getMonthlyTrend(months, signal),
  })
}

export function useIncomeVsExpenses(months: number) {
  return useQuery({
    queryKey: ['reports', 'income-vs-expenses', months],
    queryFn: ({ signal }) => getIncomeVsExpenses(months, signal),
  })
}

export function useCashFlow(months: number) {
  return useQuery({
    queryKey: ['reports', 'cash-flow', months],
    queryFn: ({ signal }) => getCashFlow(months, signal),
  })
}

export function useNetWorth() {
  return useQuery({
    queryKey: ['reports', 'net-worth'],
    queryFn: ({ signal }) => getNetWorth(signal),
  })
}

export function useTopPayees(from: string | null, to: string | null) {
  return useQuery({
    queryKey: ['reports', 'top-payees', from, to],
    queryFn: ({ signal }) => getTopPayees(from, to, signal),
  })
}
