import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import type { Account } from '../../api/accounts'
import type { CategoryGroup } from '../../api/categories'
import {
  transactionStatuses,
  type BulkInput,
  type Transaction,
  type TransactionFilter,
} from '../../api/transactions'
import { EmptyState } from '../../components/EmptyState'
import { useIsDesktop } from '../../hooks/useMediaQuery'
import { formatMoney, parseMoneyInput } from '../../lib/money'
import { useAccounts } from '../accounts/useAccounts'
import { useCategoryGroups } from '../categories/useCategories'
import { TransactionEditorDialog } from './TransactionEditorDialog'
import { useBulkTransactions, useRestoreTransaction, useTransactions } from './useTransactions'

const PAGE_SIZE = 50

const filterInputClass = 'mt-1 block w-full rounded-md border border-gray-300 px-2 py-1.5 text-sm'
const filterLabelClass = 'block text-xs font-medium text-gray-600'

const typeOptions = ['expense', 'income', 'refund', 'transfer', 'adjustment'] as const

// Filter form with its own draft state; nothing hits the API until Apply.
function FilterBar({ accounts, groups, onApply }: {
  accounts: Account[]
  groups: CategoryGroup[]
  onApply: (f: TransactionFilter) => void
}) {
  const [q, setQ] = useState('')
  const [accountId, setAccountId] = useState('')
  const [categoryId, setCategoryId] = useState('')
  const [type, setType] = useState('')
  const [status, setStatus] = useState('')
  const [payee, setPayee] = useState('')
  const [tag, setTag] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [amountMin, setAmountMin] = useState('')
  const [amountMax, setAmountMax] = useState('')
  const [error, setError] = useState<string | null>(null)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const min = amountMin.trim() === '' ? undefined : parseMoneyInput(amountMin)
    const max = amountMax.trim() === '' ? undefined : parseMoneyInput(amountMax)
    if (min === null || max === null) {
      setError('Amounts must be decimals like 12.50.')
      return
    }
    setError(null)
    const f: TransactionFilter = {}
    if (q.trim()) f.q = q.trim()
    if (accountId) f.accountId = accountId
    if (categoryId) f.categoryId = categoryId
    if (type) f.type = type
    if (status) f.status = status
    if (payee.trim()) f.payee = payee.trim()
    if (tag.trim()) f.tag = tag.trim()
    if (from) f.from = from
    if (to) f.to = to
    if (min !== undefined) f.amountMin = min
    if (max !== undefined) f.amountMax = max
    onApply(f)
  }

  const reset = () => {
    setQ('')
    setAccountId('')
    setCategoryId('')
    setType('')
    setStatus('')
    setPayee('')
    setTag('')
    setFrom('')
    setTo('')
    setAmountMin('')
    setAmountMax('')
    setError(null)
    onApply({})
  }

  return (
    <form
      onSubmit={submit}
      aria-label="Transaction filters"
      className="mt-4 rounded-lg bg-white p-3 shadow"
    >
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div className="col-span-2">
          <label htmlFor="filter-q" className={filterLabelClass}>
            Search
          </label>
          <input
            id="filter-q"
            type="search"
            placeholder="Payee, notes, or amount"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-account" className={filterLabelClass}>
            Account
          </label>
          <select
            id="filter-account"
            value={accountId}
            onChange={(e) => setAccountId(e.target.value)}
            className={`${filterInputClass} bg-white`}
          >
            <option value="">All accounts</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="filter-category" className={filterLabelClass}>
            Category
          </label>
          <select
            id="filter-category"
            value={categoryId}
            onChange={(e) => setCategoryId(e.target.value)}
            className={`${filterInputClass} bg-white`}
          >
            <option value="">All categories</option>
            {groups.flatMap((g) =>
              g.categories.map((c) => (
                <option key={c.id} value={c.id}>
                  {g.name} · {c.name}
                </option>
              )),
            )}
          </select>
        </div>
        <div>
          <label htmlFor="filter-type" className={filterLabelClass}>
            Type
          </label>
          <select
            id="filter-type"
            value={type}
            onChange={(e) => setType(e.target.value)}
            className={`${filterInputClass} bg-white`}
          >
            <option value="">All types</option>
            {typeOptions.map((t) => (
              <option key={t} value={t}>
                {t[0].toUpperCase() + t.slice(1)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="filter-status" className={filterLabelClass}>
            Status
          </label>
          <select
            id="filter-status"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className={`${filterInputClass} bg-white`}
          >
            <option value="">Any status</option>
            {transactionStatuses.map((s) => (
              <option key={s} value={s}>
                {s[0].toUpperCase() + s.slice(1)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="filter-payee" className={filterLabelClass}>
            Payee
          </label>
          <input
            id="filter-payee"
            type="text"
            value={payee}
            onChange={(e) => setPayee(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-tag" className={filterLabelClass}>
            Tag
          </label>
          <input
            id="filter-tag"
            type="text"
            value={tag}
            onChange={(e) => setTag(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-from" className={filterLabelClass}>
            From date
          </label>
          <input
            id="filter-from"
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-to" className={filterLabelClass}>
            To date
          </label>
          <input
            id="filter-to"
            type="date"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-amount-min" className={filterLabelClass}>
            Min amount
          </label>
          <input
            id="filter-amount-min"
            inputMode="decimal"
            value={amountMin}
            onChange={(e) => setAmountMin(e.target.value)}
            className={filterInputClass}
          />
        </div>
        <div>
          <label htmlFor="filter-amount-max" className={filterLabelClass}>
            Max amount
          </label>
          <input
            id="filter-amount-max"
            inputMode="decimal"
            value={amountMax}
            onChange={(e) => setAmountMax(e.target.value)}
            className={filterInputClass}
          />
        </div>
      </div>
      {error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {error}
        </p>
      ) : null}
      <div className="mt-3 flex gap-2">
        <button
          type="submit"
          className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-indigo-500"
        >
          Apply filters
        </button>
        <button
          type="button"
          onClick={reset}
          className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
        >
          Reset
        </button>
      </div>
    </form>
  )
}

// Appears when rows are selected; every action runs one bulk API call over
// the selection.
function BulkToolbar({ count, groups, pending, onAction, onClear }: {
  count: number
  groups: CategoryGroup[]
  pending: boolean
  onAction: (input: Omit<BulkInput, 'ids'>) => void
  onClear: () => void
}) {
  const [categoryId, setCategoryId] = useState('')
  const [tag, setTag] = useState('')

  return (
    <div
      role="toolbar"
      aria-label="Bulk actions"
      className="mt-4 flex flex-wrap items-center gap-2 rounded-lg bg-indigo-50 p-3"
    >
      <span className="text-sm font-semibold text-indigo-900">{count} selected</span>
      <label htmlFor="bulk-category" className="sr-only">
        Bulk category
      </label>
      <select
        id="bulk-category"
        value={categoryId}
        onChange={(e) => setCategoryId(e.target.value)}
        className="rounded-md border border-gray-300 bg-white px-2 py-1.5 text-sm"
      >
        <option value="">Choose a category…</option>
        {groups.flatMap((g) =>
          g.categories
            .filter((c) => !c.archivedAt)
            .map((c) => (
              <option key={c.id} value={c.id}>
                {g.name} · {c.name}
              </option>
            )),
        )}
      </select>
      <button
        type="button"
        disabled={pending || !categoryId}
        onClick={() => onAction({ action: 'categorize', categoryId })}
        className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
      >
        Categorize
      </button>
      <label htmlFor="bulk-tag" className="sr-only">
        Bulk tag
      </label>
      <input
        id="bulk-tag"
        type="text"
        placeholder="Tag name"
        value={tag}
        onChange={(e) => setTag(e.target.value)}
        className="w-28 rounded-md border border-gray-300 px-2 py-1.5 text-sm"
      />
      <button
        type="button"
        disabled={pending || tag.trim() === ''}
        onClick={() => onAction({ action: 'tag', tag: tag.trim() })}
        className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
      >
        Tag
      </button>
      <button
        type="button"
        disabled={pending}
        onClick={() => onAction({ action: 'mark_reviewed', reviewed: true })}
        className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
      >
        Mark reviewed
      </button>
      <button
        type="button"
        disabled={pending}
        onClick={() => onAction({ action: 'delete' })}
        className="rounded-md border border-red-200 bg-white px-3 py-1.5 text-sm font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
      >
        Delete
      </button>
      <button
        type="button"
        onClick={onClear}
        className="ml-auto rounded-md px-2 py-1.5 text-sm text-gray-600 hover:bg-white"
      >
        Clear selection
      </button>
    </div>
  )
}

function statusLabel(t: Transaction): string {
  const label = t.status[0].toUpperCase() + t.status.slice(1)
  return t.reviewed ? `${label} · Reviewed` : label
}

export function TransactionsPage() {
  const isDesktop = useIsDesktop()
  const [filter, setFilter] = useState<TransactionFilter>({})
  const [offset, setOffset] = useState(0)
  const [showDeleted, setShowDeleted] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [editor, setEditor] = useState<{ transaction: Transaction | null } | null>(null)
  const [entryWarning, setEntryWarning] = useState<string | null>(null)

  const { data: accounts } = useAccounts()
  const { data: groups } = useCategoryGroups()
  const { data: page, isLoading, error } = useTransactions({
    ...filter,
    deleted: showDeleted,
    limit: PAGE_SIZE,
    offset,
  })
  const bulk = useBulkTransactions()
  const restore = useRestoreTransaction()

  const accountById = new Map((accounts ?? []).map((a) => [a.id, a]))
  const categoryNames = new Map(
    (groups ?? []).flatMap((g) => g.categories.map((c) => [c.id, c.name] as const)),
  )

  const categoryLabel = (t: Transaction): string => {
    if (t.type === 'transfer') return 'Transfer'
    if (t.splits.length > 0) return `Split (${t.splits.length})`
    return t.categoryId ? (categoryNames.get(t.categoryId) ?? '—') : '—'
  }
  const amountText = (t: Transaction): string =>
    formatMoney(t.amount, accountById.get(t.accountId)?.currency ?? 'USD')

  const rows = page?.transactions ?? []
  const total = page?.total ?? 0
  const hasFilters = Object.keys(filter).length > 0

  const clearSelection = () => setSelected(new Set())
  const applyFilter = (f: TransactionFilter) => {
    setFilter(f)
    setOffset(0)
    clearSelection()
  }
  const toggleDeletedView = () => {
    setShowDeleted((v) => !v)
    setOffset(0)
    clearSelection()
  }
  const toggleRow = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }
  const toggleAll = () => {
    setSelected((prev) => (prev.size === rows.length ? new Set() : new Set(rows.map((t) => t.id))))
  }
  const runBulk = (input: Omit<BulkInput, 'ids'>) => {
    bulk.mutate({ ...input, ids: [...selected] }, { onSuccess: clearSelection })
  }
  const handleSaved = (saved: Transaction) => {
    if (saved.duplicateWarning) {
      const n = saved.duplicateOf?.length ?? 0
      setEntryWarning(
        `"${saved.payee}" looks like a possible duplicate of ${n} existing transaction${
          n === 1 ? '' : 's'
        } (same account and amount on a nearby date). Review it in the list.`,
      )
    } else {
      setEntryWarning(null)
    }
  }

  const selectionColumn = !showDeleted
  const pageEnd = Math.min(offset + PAGE_SIZE, total)

  const rowActions = (t: Transaction) =>
    showDeleted ? (
      <button
        type="button"
        aria-label={`Restore ${t.payee}`}
        disabled={restore.isPending}
        onClick={() => restore.mutate(t.id)}
        className="rounded-md px-2 py-1 text-sm font-medium text-indigo-600 hover:bg-indigo-50 disabled:opacity-50"
      >
        Restore
      </button>
    ) : (
      <button
        type="button"
        aria-label={`Edit ${t.payee}`}
        onClick={() => setEditor({ transaction: t })}
        className="rounded-md px-2 py-1 text-sm font-medium text-indigo-600 hover:bg-indigo-50"
      >
        Edit
      </button>
    )

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold text-gray-900">
          {showDeleted ? 'Deleted transactions' : 'Transactions'}
        </h1>
        <div className="flex gap-2">
          {!showDeleted ? (
            <Link
              to="/import"
              className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
            >
              Import CSV
            </Link>
          ) : null}
          <button
            type="button"
            onClick={toggleDeletedView}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
          >
            {showDeleted ? 'Back to transactions' : 'View deleted'}
          </button>
          {!showDeleted ? (
            <button
              type="button"
              onClick={() => setEditor({ transaction: null })}
              className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              Add transaction
            </button>
          ) : null}
        </div>
      </div>

      {entryWarning ? (
        <div
          role="status"
          className="mt-4 flex items-start justify-between gap-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900"
        >
          <p>
            <span aria-hidden="true" className="mr-1">
              ⚠️
            </span>
            {entryWarning}
          </p>
          <button
            type="button"
            onClick={() => setEntryWarning(null)}
            className="rounded-md px-2 py-0.5 font-medium text-amber-900 hover:bg-amber-100"
          >
            Dismiss
          </button>
        </div>
      ) : null}

      <FilterBar accounts={accounts ?? []} groups={groups ?? []} onApply={applyFilter} />

      {selected.size > 0 && !showDeleted ? (
        <BulkToolbar
          count={selected.size}
          groups={groups ?? []}
          pending={bulk.isPending}
          onAction={runBulk}
          onClear={clearSelection}
        />
      ) : null}
      {bulk.error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {bulk.error.message}
        </p>
      ) : null}

      {isLoading ? <p className="mt-6 text-sm text-gray-600">Loading…</p> : null}
      {error ? (
        <p role="alert" className="mt-6 text-sm text-red-600">
          {error.message}
        </p>
      ) : null}

      {!isLoading && total === 0 ? (
        showDeleted || hasFilters ? (
          <p className="mt-6 text-sm text-gray-600">
            {showDeleted
              ? 'No deleted transactions.'
              : 'No transactions match your filters.'}
          </p>
        ) : (
          <EmptyState
            title="No transactions yet"
            description="Record income, expenses, and transfers here — or import a CSV from your bank to get started quickly."
          >
            <button
              type="button"
              onClick={() => setEditor({ transaction: null })}
              className="mt-4 rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              Add your first transaction
            </button>
          </EmptyState>
        )
      ) : null}

      {rows.length > 0 && isDesktop ? (
        <table className="mt-4 w-full rounded-lg bg-white shadow">
          <thead>
            <tr className="border-b border-gray-200 text-left text-xs font-semibold uppercase tracking-wide text-gray-500">
              {selectionColumn ? (
                <th scope="col" className="w-10 px-4 py-3">
                  <input
                    type="checkbox"
                    aria-label="Select all on page"
                    checked={rows.length > 0 && selected.size === rows.length}
                    onChange={toggleAll}
                    className="h-4 w-4 rounded"
                  />
                </th>
              ) : null}
              <th scope="col" className="px-4 py-3">
                Date
              </th>
              <th scope="col" className="px-4 py-3">
                Payee
              </th>
              <th scope="col" className="px-4 py-3">
                Category
              </th>
              <th scope="col" className="px-4 py-3">
                Account
              </th>
              <th scope="col" className="px-4 py-3 text-right">
                Amount
              </th>
              <th scope="col" className="px-4 py-3">
                Status
              </th>
              <th scope="col" className="px-4 py-3">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((t) => (
              <tr key={t.id} className="border-t border-gray-100">
                {selectionColumn ? (
                  <td className="px-4 py-2">
                    <input
                      type="checkbox"
                      aria-label={`Select ${t.payee}`}
                      checked={selected.has(t.id)}
                      onChange={() => toggleRow(t.id)}
                      className="h-4 w-4 rounded"
                    />
                  </td>
                ) : null}
                <td className="whitespace-nowrap px-4 py-2 tabular-nums text-gray-700">{t.date}</td>
                <td className="px-4 py-2 text-gray-900">
                  {t.payee || '—'}
                  {t.tags.map((tag) => (
                    <span
                      key={tag.id}
                      className="ml-2 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600"
                    >
                      {tag.name}
                    </span>
                  ))}
                </td>
                <td className="px-4 py-2 text-gray-700">{categoryLabel(t)}</td>
                <td className="px-4 py-2 text-gray-700">{accountById.get(t.accountId)?.name ?? '—'}</td>
                <td
                  className={`px-4 py-2 text-right tabular-nums ${
                    t.amount < 0 ? 'text-gray-900' : 'text-green-700'
                  }`}
                >
                  {amountText(t)}
                </td>
                <td className="px-4 py-2 text-sm text-gray-600">{statusLabel(t)}</td>
                <td className="px-4 py-2 text-right">{rowActions(t)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}

      {rows.length > 0 && !isDesktop ? (
        <div className="mt-4 space-y-2">
          {rows.map((t) => (
            <div key={t.id} className="rounded-lg bg-white p-3 shadow">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  {selectionColumn ? (
                    <input
                      type="checkbox"
                      aria-label={`Select ${t.payee}`}
                      checked={selected.has(t.id)}
                      onChange={() => toggleRow(t.id)}
                      className="h-4 w-4 rounded"
                    />
                  ) : null}
                  <p className="font-medium text-gray-900">{t.payee || '—'}</p>
                </div>
                <p
                  className={`tabular-nums ${t.amount < 0 ? 'text-gray-900' : 'text-green-700'}`}
                >
                  {amountText(t)}
                </p>
              </div>
              <p className="mt-1 text-sm text-gray-600">
                {t.date} · {categoryLabel(t)} · {accountById.get(t.accountId)?.name ?? '—'}
              </p>
              <div className="mt-1 flex items-center justify-between">
                <p className="text-xs text-gray-500">{statusLabel(t)}</p>
                {rowActions(t)}
              </div>
            </div>
          ))}
        </div>
      ) : null}

      {total > PAGE_SIZE ? (
        <nav aria-label="Pagination" className="mt-4 flex items-center justify-between">
          <p className="text-sm text-gray-600">
            {offset + 1}–{pageEnd} of {total}
          </p>
          <div className="flex gap-2">
            <button
              type="button"
              disabled={offset === 0}
              onClick={() => {
                setOffset(Math.max(0, offset - PAGE_SIZE))
                clearSelection()
              }}
              className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50"
            >
              Previous
            </button>
            <button
              type="button"
              disabled={pageEnd >= total}
              onClick={() => {
                setOffset(offset + PAGE_SIZE)
                clearSelection()
              }}
              className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50"
            >
              Next
            </button>
          </div>
        </nav>
      ) : null}

      {editor ? (
        <TransactionEditorDialog
          accounts={accounts ?? []}
          groups={groups ?? []}
          transaction={editor.transaction}
          onClose={() => setEditor(null)}
          onSaved={handleSaved}
        />
      ) : null}
    </>
  )
}
