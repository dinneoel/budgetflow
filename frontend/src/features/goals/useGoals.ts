import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  addGoalContribution,
  archiveGoal,
  createGoal,
  listGoals,
  updateGoal,
  type ContributionInput,
  type GoalInput,
} from '../../api/goals'

export const goalsQueryKey = ['goals'] as const

export function useGoals() {
  return useQuery({
    queryKey: goalsQueryKey,
    queryFn: ({ signal }) => listGoals(signal),
  })
}

function useInvalidateGoals() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: goalsQueryKey })
}

export function useCreateGoal() {
  const invalidate = useInvalidateGoals()
  return useMutation({
    mutationFn: (input: GoalInput) => createGoal(input),
    onSuccess: invalidate,
  })
}

export function useUpdateGoal(id: string) {
  const invalidate = useInvalidateGoals()
  return useMutation({
    mutationFn: (input: GoalInput) => updateGoal(id, input),
    onSuccess: invalidate,
  })
}

export function useArchiveGoal() {
  const invalidate = useInvalidateGoals()
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) => archiveGoal(id, archived),
    onSuccess: invalidate,
  })
}

export function useAddContribution(goalId: string) {
  const invalidate = useInvalidateGoals()
  return useMutation({
    mutationFn: (input: ContributionInput) => addGoalContribution(goalId, input),
    onSuccess: invalidate,
  })
}
