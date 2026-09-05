import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  archiveAccount,
  createAccount,
  getAccount,
  listAccounts,
  reconcileAccount,
  updateAccount,
  type AccountInput,
} from '../../api/accounts'

export const accountsQueryKey = ['accounts'] as const

export function useAccounts() {
  return useQuery({
    queryKey: accountsQueryKey,
    queryFn: ({ signal }) => listAccounts(signal),
  })
}

export function useAccount(id: string) {
  return useQuery({
    queryKey: [...accountsQueryKey, id],
    queryFn: ({ signal }) => getAccount(id, signal),
  })
}

function useInvalidateAccounts() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: accountsQueryKey })
}

export function useCreateAccount() {
  const invalidate = useInvalidateAccounts()
  return useMutation({
    mutationFn: (input: AccountInput) => createAccount(input),
    onSuccess: invalidate,
  })
}

export function useUpdateAccount(id: string) {
  const invalidate = useInvalidateAccounts()
  return useMutation({
    mutationFn: (input: AccountInput) => updateAccount(id, input),
    onSuccess: invalidate,
  })
}

export function useArchiveAccount(id: string) {
  const invalidate = useInvalidateAccounts()
  return useMutation({
    mutationFn: (archived: boolean) => archiveAccount(id, archived),
    onSuccess: invalidate,
  })
}

export function useReconcileAccount(id: string) {
  const invalidate = useInvalidateAccounts()
  return useMutation({
    mutationFn: (statementBalance: number) => reconcileAccount(id, statementBalance),
    onSuccess: invalidate,
  })
}
