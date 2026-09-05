import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  archiveCategory,
  archiveGroup,
  createCategory,
  createGroup,
  listCategoryGroups,
  mergeCategory,
  reorderCategories,
  reorderGroups,
  seedDefaultCategories,
  updateCategory,
  updateGroup,
  type CategoryInput,
} from '../../api/categories'

export const categoriesQueryKey = ['categories'] as const

export function useCategoryGroups() {
  return useQuery({
    queryKey: categoriesQueryKey,
    queryFn: ({ signal }) => listCategoryGroups(signal),
  })
}

function useInvalidateCategories() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: categoriesQueryKey })
}

export function useCreateGroup() {
  const invalidate = useInvalidateCategories()
  return useMutation({ mutationFn: (name: string) => createGroup(name), onSuccess: invalidate })
}

export function useUpdateGroup() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => updateGroup(id, name),
    onSuccess: invalidate,
  })
}

export function useArchiveGroup() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) => archiveGroup(id, archived),
    onSuccess: invalidate,
  })
}

export function useReorderGroups() {
  const invalidate = useInvalidateCategories()
  return useMutation({ mutationFn: (ids: string[]) => reorderGroups(ids), onSuccess: invalidate })
}

export function useCreateCategory() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: (input: CategoryInput) => createCategory(input),
    onSuccess: invalidate,
  })
}

export function useUpdateCategory() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: CategoryInput }) => updateCategory(id, input),
    onSuccess: invalidate,
  })
}

export function useArchiveCategory() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) =>
      archiveCategory(id, archived),
    onSuccess: invalidate,
  })
}

export function useReorderCategories() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: (ids: string[]) => reorderCategories(ids),
    onSuccess: invalidate,
  })
}

export function useMergeCategory() {
  const invalidate = useInvalidateCategories()
  return useMutation({
    mutationFn: ({ sourceId, targetId }: { sourceId: string; targetId: string }) =>
      mergeCategory(sourceId, targetId),
    onSuccess: invalidate,
  })
}

export function useSeedDefaults() {
  const invalidate = useInvalidateCategories()
  return useMutation({ mutationFn: () => seedDefaultCategories(), onSuccess: invalidate })
}
