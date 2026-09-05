import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError } from '../../api/client'
import {
  createPeriod,
  getPeriod,
  listPeriods,
  setAllocation,
  updatePeriod,
  type BudgetPeriodDetail,
  type CreatePeriodInput,
  type UpdatePeriodInput,
} from '../../api/budgets'

export const budgetsListKey = ['budgets'] as const

export const periodQueryKey = (year: number, month: number) => ['budget', year, month] as const

export function usePeriods() {
  return useQuery({
    queryKey: budgetsListKey,
    queryFn: ({ signal }) => listPeriods(signal),
  })
}

// Resolves to the month's budget detail, or null when it has not been created.
export function usePeriod(year: number, month: number) {
  return useQuery<BudgetPeriodDetail | null>({
    queryKey: periodQueryKey(year, month),
    queryFn: async ({ signal }) => {
      try {
        return await getPeriod(year, month, signal)
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) {
          return null
        }
        throw err
      }
    },
  })
}

// Every budget mutation returns the recomputed period detail; store it so the
// page updates without a refetch.
function useApplyDetail() {
  const queryClient = useQueryClient()
  return (detail: BudgetPeriodDetail) => {
    queryClient.setQueryData(periodQueryKey(detail.year, detail.month), detail)
    void queryClient.invalidateQueries({ queryKey: budgetsListKey })
  }
}

export function useCreatePeriod() {
  const apply = useApplyDetail()
  return useMutation({
    mutationFn: (input: CreatePeriodInput) => createPeriod(input),
    onSuccess: apply,
  })
}

export function useUpdatePeriod() {
  const apply = useApplyDetail()
  return useMutation({
    mutationFn: ({ periodId, input }: { periodId: string; input: UpdatePeriodInput }) =>
      updatePeriod(periodId, input),
    onSuccess: apply,
  })
}

export function useSetAllocation() {
  const apply = useApplyDetail()
  return useMutation({
    mutationFn: ({
      periodId,
      categoryId,
      amount,
    }: {
      periodId: string
      categoryId: string
      amount: number
    }) => setAllocation(periodId, categoryId, amount),
    onSuccess: apply,
  })
}
