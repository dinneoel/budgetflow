import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError } from '../../api/client'
import {
  fetchCurrentUser,
  signIn,
  signOut,
  signOutAll,
  signUp,
  updateProfile,
  type ProfileUpdateInput,
  type SignUpInput,
  type User,
} from '../../api/auth'

export const meQueryKey = ['auth', 'me'] as const

// Resolves to the signed-in user, or null when there is no valid session.
export function useCurrentUser() {
  return useQuery<User | null>({
    queryKey: meQueryKey,
    queryFn: async ({ signal }) => {
      try {
        return await fetchCurrentUser(signal)
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          return null
        }
        throw err
      }
    },
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
}

export function useSignIn() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      signIn(email, password),
    onSuccess: (user) => queryClient.setQueryData(meQueryKey, user),
  })
}

export function useSignUp() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: SignUpInput) => signUp(input),
    onSuccess: (user) => queryClient.setQueryData(meQueryKey, user),
  })
}

export function useSignOut() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: signOut,
    onSettled: () => {
      queryClient.clear()
      queryClient.setQueryData(meQueryKey, null)
    },
  })
}

export function useSignOutAll() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: signOutAll,
    onSettled: () => {
      queryClient.clear()
      queryClient.setQueryData(meQueryKey, null)
    },
  })
}

export function useUpdateProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: ProfileUpdateInput) => updateProfile(input),
    onSuccess: (user) => queryClient.setQueryData(meQueryKey, user),
  })
}
