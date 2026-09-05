import { get, post, put } from './client'

export const accountTypes = [
  'cash',
  'checking',
  'savings',
  'credit_card',
  'ewallet',
  'custom',
] as const

export type AccountType = (typeof accountTypes)[number]

export interface Account {
  id: string
  name: string
  institution: string
  type: AccountType
  currency: string
  openingBalance: number
  balance: number
  includeInNetWorth: boolean
  archivedAt: string | null
  createdAt: string
  updatedAt: string
}

export interface AccountInput {
  name: string
  institution: string
  type: AccountType
  currency: string
  openingBalance: number
  includeInNetWorth: boolean
}

export interface ReconcileResult {
  account: Account
  adjustment: {
    id: string
    type: string
    amount: number
    date: string
    payee: string
    notes: string
  } | null
}

export async function listAccounts(signal?: AbortSignal): Promise<Account[]> {
  const res = await get<{ accounts: Account[] }>('/accounts', signal)
  return res.accounts
}

export async function getAccount(id: string, signal?: AbortSignal): Promise<Account> {
  const res = await get<{ account: Account }>(`/accounts/${id}`, signal)
  return res.account
}

export async function createAccount(input: AccountInput): Promise<Account> {
  const res = await post<{ account: Account }>('/accounts', input)
  return res.account
}

export async function updateAccount(id: string, input: AccountInput): Promise<Account> {
  const res = await put<{ account: Account }>(`/accounts/${id}`, input)
  return res.account
}

export async function archiveAccount(id: string, archived: boolean): Promise<Account> {
  const res = await post<{ account: Account }>(
    `/accounts/${id}/${archived ? 'archive' : 'unarchive'}`,
  )
  return res.account
}

export async function reconcileAccount(
  id: string,
  statementBalance: number,
): Promise<ReconcileResult> {
  return post<ReconcileResult>(`/accounts/${id}/reconcile`, { statementBalance })
}
