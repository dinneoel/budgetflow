import { useQuery } from '@tanstack/react-query'
import { getDashboard } from '../../api/reports'

export const dashboardQueryKey = ['dashboard'] as const

export function useDashboard() {
  return useQuery({
    queryKey: dashboardQueryKey,
    queryFn: ({ signal }) => getDashboard(signal),
  })
}
