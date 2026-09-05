import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  commitImport,
  deleteImportBatch,
  getImportPreview,
  listImportBatches,
  setImportMapping,
  uploadImport,
  type CommitInput,
  type ImportMapping,
} from '../../api/imports'

export const importBatchesQueryKey = ['imports'] as const

export function useImportBatches() {
  return useQuery({
    queryKey: importBatchesQueryKey,
    queryFn: ({ signal }) => listImportBatches(signal),
  })
}

export function useUploadImport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (file: File) => uploadImport(file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: importBatchesQueryKey }),
  })
}

export function useSetImportMapping(batchId: string | null) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (mapping: ImportMapping) => setImportMapping(batchId ?? '', mapping),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ['imports', batchId, 'preview'] }),
  })
}

export function useImportPreview(batchId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: ['imports', batchId, 'preview'],
    queryFn: ({ signal }) => getImportPreview(batchId ?? '', signal),
    enabled: enabled && batchId !== null,
  })
}

// Committing creates transactions, so downstream screens must refetch.
export function useCommitImport(batchId: string | null) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CommitInput) => commitImport(batchId ?? '', input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: importBatchesQueryKey })
      queryClient.invalidateQueries({ queryKey: ['transactions'] })
      queryClient.invalidateQueries({ queryKey: ['accounts'] })
      queryClient.invalidateQueries({ queryKey: ['budget'] })
      queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
  })
}

export function useDeleteImportBatch() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (batchId: string) => deleteImportBatch(batchId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: importBatchesQueryKey })
      queryClient.invalidateQueries({ queryKey: ['transactions'] })
      queryClient.invalidateQueries({ queryKey: ['accounts'] })
      queryClient.invalidateQueries({ queryKey: ['budget'] })
      queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
  })
}
