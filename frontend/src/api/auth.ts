import { get, post, put, setCsrfToken } from './client'

export interface User {
  id: string
  email: string
  name: string
  locale: string
  timeZone: string
  firstDayOfWeek: number
  defaultCurrency: string
  createdAt: string
}

export interface SessionInfo {
  id: string
  userAgent: string
  ipAddress: string
  createdAt: string
  expiresAt: string
  current: boolean
}

interface AuthResponse {
  user: User
  csrfToken: string
}

export interface SignUpInput {
  email: string
  password: string
  name: string
  defaultCurrency: string
}

export interface ProfileUpdateInput {
  name: string
  locale: string
  timeZone: string
  firstDayOfWeek: number
  defaultCurrency: string
}

export async function signUp(input: SignUpInput): Promise<User> {
  const res = await post<AuthResponse>('/auth/sign-up', input)
  setCsrfToken(res.csrfToken)
  return res.user
}

export async function signIn(email: string, password: string): Promise<User> {
  const res = await post<AuthResponse>('/auth/sign-in', { email, password })
  setCsrfToken(res.csrfToken)
  return res.user
}

export async function fetchCurrentUser(signal?: AbortSignal): Promise<User> {
  const res = await get<AuthResponse>('/auth/me', signal)
  setCsrfToken(res.csrfToken)
  return res.user
}

export async function signOut(): Promise<void> {
  await post<void>('/auth/sign-out')
  setCsrfToken(null)
}

export async function signOutAll(): Promise<void> {
  await post<void>('/auth/sign-out-all')
  setCsrfToken(null)
}

export async function listSessions(signal?: AbortSignal): Promise<SessionInfo[]> {
  const res = await get<{ sessions: SessionInfo[] }>('/auth/sessions', signal)
  return res.sessions
}

export async function updateProfile(input: ProfileUpdateInput): Promise<User> {
  const res = await put<{ user: User }>('/profile', input)
  return res.user
}

export async function requestPasswordReset(email: string): Promise<void> {
  await post<{ status: string }>('/auth/password-reset/request', { email })
}

export async function confirmPasswordReset(token: string, password: string): Promise<void> {
  await post<void>('/auth/password-reset/confirm', { token, password })
}
