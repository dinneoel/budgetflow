import { del, get, post, put } from './client'

export const transactionTypes = ['expense', 'income', 'refund', 'adjustment'] as const
export type TransactionType = (typeof transactionTypes)[number]

export const transactionStatuses = ['uncleared', 'cleared', 'reconciled'] as const
export type TransactionStatus = (typeof transactionStatuses)[number]

export interface TransactionSplit {
  id: string
  categoryId: string
  amount: number
  memo: string
}

export interface TransactionTag {
  id: string
  name: string
}

// Amounts are signed int64 minor units: expenses and transfer-out legs are
// negative; income, refunds, and transfer-in legs are positive.
export interface Transaction {
  id: string
  accountId: string
  categoryId: string | null
  type: TransactionType | 'transfer'
  status: TransactionStatus
  amount: number
  date: string
  payee: string
  notes: string
  reviewed: boolean
  transferPairId: string | null
  deletedAt: string | null
  createdAt: string
  updatedAt: string
  splits: TransactionSplit[]
  tags: TransactionTag[]
  duplicateOf?: string[]
  duplicateWarning?: boolean
}

export interface SplitInput {
  categoryId: string
  amount: number
  memo: string
}

// The API takes positive magnitudes and derives the ledger sign from the
// type; adjustments accept any non-zero signed amount. 'transfer' is only
// valid when updating an existing transfer leg.
export interface TransactionInput {
  accountId: string
  categoryId: string | null
  type: TransactionType | 'transfer'
  status: TransactionStatus
  amount: number
  date: string
  payee: string
  notes: string
  reviewed: boolean
  splits: SplitInput[]
  tags: string[]
}

export interface TransferInput {
  fromAccountId: string
  toAccountId: string
  amount: number
  date: string
  notes: string
}

export interface TransactionFilter {
  accountId?: string
  categoryId?: string
  from?: string
  to?: string
  type?: string
  status?: string
  payee?: string
  tag?: string
  q?: string
  amountMin?: number
  amountMax?: number
  deleted?: boolean
  limit?: number
  offset?: number
}

export interface TransactionPage {
  transactions: Transaction[]
  total: number
  limit: number
  offset: number
}

export interface BulkInput {
  action: 'categorize' | 'tag' | 'delete' | 'mark_reviewed'
  ids: string[]
  categoryId?: string
  tag?: string
  reviewed?: boolean
}

export interface BulkResult {
  action: string
  affectedIds: string[]
}

export function filterToQuery(f: TransactionFilter): string {
  const params = new URLSearchParams()
  const set = (key: string, value: string | number | undefined) => {
    if (value !== undefined && value !== '') {
      params.set(key, String(value))
    }
  }
  set('accountId', f.accountId)
  set('categoryId', f.categoryId)
  set('from', f.from)
  set('to', f.to)
  set('type', f.type)
  set('status', f.status)
  set('payee', f.payee)
  set('tag', f.tag)
  set('q', f.q)
  set('amountMin', f.amountMin)
  set('amountMax', f.amountMax)
  if (f.deleted) {
    set('deleted', 'true')
  }
  set('limit', f.limit)
  set('offset', f.offset)
  return params.toString()
}

export async function listTransactions(
  filter: TransactionFilter,
  signal?: AbortSignal,
): Promise<TransactionPage> {
  const qs = filterToQuery(filter)
  return get<TransactionPage>(`/transactions${qs ? `?${qs}` : ''}`, signal)
}

export async function createTransaction(input: TransactionInput): Promise<Transaction> {
  const res = await post<{ transaction: Transaction }>('/transactions', input)
  return res.transaction
}

export async function createTransfer(
  input: TransferInput,
): Promise<{ outTransaction: Transaction; inTransaction: Transaction }> {
  return post('/transactions/transfer', input)
}

export async function updateTransaction(id: string, input: TransactionInput): Promise<Transaction> {
  const res = await put<{ transaction: Transaction }>(`/transactions/${id}`, input)
  return res.transaction
}

export async function deleteTransaction(id: string): Promise<void> {
  await del(`/transactions/${id}`)
}

export async function restoreTransaction(id: string): Promise<Transaction> {
  const res = await post<{ transaction: Transaction }>(`/transactions/${id}/restore`)
  return res.transaction
}

export async function bulkTransactions(input: BulkInput): Promise<BulkResult> {
  return post<BulkResult>('/transactions/bulk', input)
}
