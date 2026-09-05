import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  archiveRecurringRule,
  createRecurringRule,
  listRecurringRules,
  markRecurringRulePaid,
  matchRecurringRule,
  updateRecurringRule,
  type MarkPaidInput,
  type RecurringRuleInput,
} from '../../api/recurring'

export const recurringQueryKey = ['recurring'] as const

export function useRecurringRules() {
  return useQuery({
    queryKey: recurringQueryKey,
    queryFn: ({ signal }) => listRecurringRules(signal),
  })
}

function useInvalidateRecurring() {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({ queryKey: recurringQueryKey })
    // Paying or matching a bill writes a transaction and shifts reserved
    // amounts, so budget and transaction views are stale too.
    queryClient.invalidateQueries({ queryKey: ['transactions'] })
    queryClient.invalidateQueries({ queryKey: ['budget'] })
  }
}

export function useCreateRecurringRule() {
  const invalidate = useInvalidateRecurring()
  return useMutation({
    mutationFn: (input: RecurringRuleInput) => createRecurringRule(input),
    onSuccess: invalidate,
  })
}

export function useUpdateRecurringRule(id: string) {
  const invalidate = useInvalidateRecurring()
  return useMutation({
    mutationFn: (input: RecurringRuleInput) => updateRecurringRule(id, input),
    onSuccess: invalidate,
  })
}

export function useArchiveRecurringRule() {
  const invalidate = useInvalidateRecurring()
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) =>
      archiveRecurringRule(id, archived),
    onSuccess: invalidate,
  })
}

export function useMarkRulePaid() {
  const invalidate = useInvalidateRecurring()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input?: MarkPaidInput }) =>
      markRecurringRulePaid(id, input),
    onSuccess: invalidate,
  })
}

export function useMatchRule() {
  const invalidate = useInvalidateRecurring()
  return useMutation({
    mutationFn: ({ id, transactionId }: { id: string; transactionId: string }) =>
      matchRecurringRule(id, transactionId),
    onSuccess: invalidate,
  })
}
