import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  bulkTransactions,
  createTransaction,
  createTransfer,
  deleteTransaction,
  filterToQuery,
  listTransactions,
  restoreTransaction,
  updateTransaction,
  type BulkInput,
  type TransactionFilter,
  type TransactionInput,
  type TransferInput,
} from '../../api/transactions'

export const transactionsQueryKey = ['transactions'] as const

export function useTransactions(filter: TransactionFilter) {
  return useQuery({
    // Keyed on the serialized query so equal filters share a cache entry.
    queryKey: [...transactionsQueryKey, filterToQuery(filter)],
    queryFn: ({ signal }) => listTransactions(filter, signal),
  })
}

function useInvalidateTransactions() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: transactionsQueryKey })
}

export function useCreateTransaction() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: (input: TransactionInput) => createTransaction(input),
    onSuccess: invalidate,
  })
}

export function useCreateTransfer() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: (input: TransferInput) => createTransfer(input),
    onSuccess: invalidate,
  })
}

export function useUpdateTransaction() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: TransactionInput }) =>
      updateTransaction(id, input),
    onSuccess: invalidate,
  })
}

export function useDeleteTransaction() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: (id: string) => deleteTransaction(id),
    onSuccess: invalidate,
  })
}

export function useRestoreTransaction() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: (id: string) => restoreTransaction(id),
    onSuccess: invalidate,
  })
}

export function useBulkTransactions() {
  const invalidate = useInvalidateTransactions()
  return useMutation({
    mutationFn: (input: BulkInput) => bulkTransactions(input),
    onSuccess: invalidate,
  })
}
