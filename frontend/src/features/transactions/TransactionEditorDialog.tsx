import { useState, type FormEvent } from 'react'
import type { Account } from '../../api/accounts'
import { ModalDialog } from '../../components/ModalDialog'
import type { CategoryGroup } from '../../api/categories'
import type { SplitInput, Transaction, TransactionInput } from '../../api/transactions'
import { formatMoney, minorToInputString, parseMoneyInput } from '../../lib/money'
import {
  useCreateTransaction,
  useCreateTransfer,
  useDeleteTransaction,
  useUpdateTransaction,
} from './useTransactions'

type Kind = 'expense' | 'income' | 'transfer' | 'refund' | 'adjustment'

const kindOptions: { value: Kind; label: string }[] = [
  { value: 'expense', label: 'Expense' },
  { value: 'income', label: 'Income' },
  { value: 'transfer', label: 'Transfer' },
  { value: 'refund', label: 'Refund' },
]

function todayString(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
    d.getDate(),
  ).padStart(2, '0')}`
}

function parseTags(input: string): string[] {
  return input
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t !== '')
}

interface SplitRow {
  categoryId: string
  amount: string
  memo: string
}

const inputClass = 'mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm'
const labelClass = 'block text-sm font-medium text-gray-700'

// Quick add/edit modal. Keyboard-first: the amount field is focused on open,
// Enter submits, Escape closes. Splits carry a running remainder and the form
// refuses to save until they sum exactly to the transaction amount. A transfer
// leg being edited only exposes amount, date, and notes — the backend mirrors
// those onto the pair leg.
export function TransactionEditorDialog({
  accounts,
  groups,
  transaction,
  onClose,
  onSaved,
}: {
  accounts: Account[]
  groups: CategoryGroup[]
  transaction: Transaction | null
  onClose: () => void
  onSaved: (saved: Transaction) => void
}) {
  const create = useCreateTransaction()
  const createTransfer = useCreateTransfer()
  const update = useUpdateTransaction()
  const remove = useDeleteTransaction()

  const editing = transaction
  const isTransferLeg = editing?.type === 'transfer'
  const isAdjustment = editing?.type === 'adjustment'

  const [kind, setKind] = useState<Kind>((editing?.type as Kind) ?? 'expense')
  const [date, setDate] = useState(editing?.date ?? todayString())
  const [amountInput, setAmountInput] = useState(
    editing ? minorToInputString(isAdjustment ? editing.amount : Math.abs(editing.amount)) : '',
  )
  const [accountId, setAccountId] = useState(editing?.accountId ?? accounts[0]?.id ?? '')
  const [payee, setPayee] = useState(editing?.payee ?? '')
  const [categoryId, setCategoryId] = useState(editing?.categoryId ?? '')
  const [notes, setNotes] = useState(editing?.notes ?? '')
  const [tagsInput, setTagsInput] = useState(editing?.tags.map((t) => t.name).join(', ') ?? '')
  const [fromAccountId, setFromAccountId] = useState(accounts[0]?.id ?? '')
  const [toAccountId, setToAccountId] = useState('')
  const [splits, setSplits] = useState<SplitRow[]>(
    editing?.splits.map((s) => ({
      categoryId: s.categoryId,
      amount: minorToInputString(Math.abs(s.amount)),
      memo: s.memo,
    })) ?? [],
  )
  const [error, setError] = useState<string | null>(null)

  const pending =
    create.isPending || createTransfer.isPending || update.isPending || remove.isPending

  const categoryOptions = groups.flatMap((g) =>
    g.categories
      .filter((c) => !c.archivedAt)
      .map((c) => ({ id: c.id, label: `${g.name} · ${c.name}` })),
  )

  const amountMinor = parseMoneyInput(amountInput)
  const splitSum = splits.reduce((sum, s) => sum + (parseMoneyInput(s.amount) ?? 0), 0)
  const remainder = (amountMinor ?? 0) - splitSum
  const currency =
    accounts.find((a) => a.id === (kind === 'transfer' && !editing ? fromAccountId : accountId))
      ?.currency ?? 'USD'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (amountMinor === null || (kind === 'adjustment' ? amountMinor === 0 : amountMinor <= 0)) {
      setError('Enter a positive amount like 12.50.')
      return
    }
    try {
      if (kind === 'transfer') {
        if (!editing) {
          if (!fromAccountId || !toAccountId || fromAccountId === toAccountId) {
            setError('Choose two different accounts.')
            return
          }
          const res = await createTransfer.mutateAsync({
            fromAccountId,
            toAccountId,
            amount: amountMinor,
            date,
            notes,
          })
          onSaved(res.outTransaction)
        } else {
          const saved = await update.mutateAsync({
            id: editing.id,
            input: {
              accountId: editing.accountId,
              categoryId: null,
              type: 'transfer',
              status: editing.status,
              amount: amountMinor,
              date,
              payee: editing.payee,
              notes,
              reviewed: editing.reviewed,
              splits: [],
              tags: parseTags(tagsInput),
            },
          })
          onSaved(saved)
        }
        onClose()
        return
      }

      const splitInputs: SplitInput[] = []
      if (splits.length > 0) {
        for (const [i, s] of splits.entries()) {
          const minor = parseMoneyInput(s.amount)
          if (!s.categoryId) {
            setError(`Choose a category for split ${i + 1}.`)
            return
          }
          if (minor === null || minor <= 0) {
            setError(`Enter a positive amount for split ${i + 1}.`)
            return
          }
          splitInputs.push({ categoryId: s.categoryId, amount: minor, memo: s.memo })
        }
        if (remainder !== 0) {
          setError(
            `Split amounts must add up to the transaction amount — ${formatMoney(
              remainder,
              currency,
            )} left to assign.`,
          )
          return
        }
      } else if (!categoryId) {
        setError('Choose a category.')
        return
      }

      const input: TransactionInput = {
        accountId,
        categoryId: splits.length > 0 ? null : categoryId,
        type: kind,
        status: editing?.status ?? 'uncleared',
        amount: amountMinor,
        date,
        payee,
        notes,
        reviewed: editing?.reviewed ?? false,
        splits: splitInputs,
        tags: parseTags(tagsInput),
      }
      const saved = editing
        ? await update.mutateAsync({ id: editing.id, input })
        : await create.mutateAsync(input)
      onSaved(saved)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Saving failed.')
    }
  }

  const deleteTransaction = async () => {
    if (!editing) {
      return
    }
    try {
      await remove.mutateAsync(editing.id)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Deleting failed.')
    }
  }

  const accountSelect = (
    id: string,
    label: string,
    value: string,
    onChange: (v: string) => void,
  ) => (
    <div>
      <label htmlFor={id} className={labelClass}>
        {label}
      </label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`${inputClass} bg-white`}
      >
        <option value="">Choose an account…</option>
        {accounts
          .filter((a) => !a.archivedAt)
          .map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
      </select>
    </div>
  )

  return (
    <ModalDialog
      label={editing ? 'Edit transaction' : 'Add transaction'}
      onClose={onClose}
      className="max-w-lg"
    >
      <h2 className="text-lg font-semibold text-gray-900">
        {editing ? 'Edit transaction' : 'Add transaction'}
      </h2>

      {!isTransferLeg && !isAdjustment ? (
        <div role="group" aria-label="Transaction type" className="mt-3 flex gap-1">
          {kindOptions.map((o) => (
            <button
              key={o.value}
              type="button"
              aria-pressed={kind === o.value}
              // An existing transaction cannot become a transfer.
              disabled={editing !== null && o.value === 'transfer'}
              onClick={() => {
                setKind(o.value)
                setError(null)
              }}
              className={`rounded-md px-3 py-1.5 text-sm font-medium disabled:opacity-40 ${
                kind === o.value
                  ? 'bg-indigo-600 text-white'
                  : 'border border-gray-300 text-gray-700 hover:bg-gray-100'
              }`}
            >
              {o.label}
            </button>
          ))}
        </div>
      ) : null}

      <form onSubmit={submit} className="mt-3 space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label htmlFor="tx-date" className={labelClass}>
              Date
            </label>
            <input
              id="tx-date"
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              className={inputClass}
            />
          </div>
          <div>
            <label htmlFor="tx-amount" className={labelClass}>
              Amount
            </label>
            <input
              id="tx-amount"
              autoFocus
              inputMode="decimal"
              placeholder="0.00"
              value={amountInput}
              onChange={(e) => {
                setAmountInput(e.target.value)
                setError(null)
              }}
              className={inputClass}
            />
          </div>
        </div>

        {kind === 'transfer' && !editing ? (
          <div className="grid grid-cols-2 gap-3">
            {accountSelect('tx-from-account', 'From account', fromAccountId, setFromAccountId)}
            {accountSelect('tx-to-account', 'To account', toAccountId, setToAccountId)}
          </div>
        ) : null}

        {kind !== 'transfer' ? (
          <>
            {accountSelect('tx-account', 'Account', accountId, setAccountId)}
            <div>
              <label htmlFor="tx-payee" className={labelClass}>
                Payee
              </label>
              <input
                id="tx-payee"
                type="text"
                value={payee}
                onChange={(e) => setPayee(e.target.value)}
                className={inputClass}
              />
            </div>

            {splits.length === 0 ? (
              <div>
                <label htmlFor="tx-category" className={labelClass}>
                  Category
                </label>
                <select
                  id="tx-category"
                  value={categoryId}
                  onChange={(e) => setCategoryId(e.target.value)}
                  className={`${inputClass} bg-white`}
                >
                  <option value="">Choose a category…</option>
                  {categoryOptions.map((o) => (
                    <option key={o.id} value={o.id}>
                      {o.label}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={() =>
                    setSplits([
                      { categoryId, amount: '', memo: '' },
                      { categoryId: '', amount: '', memo: '' },
                    ])
                  }
                  className="mt-2 rounded-md px-2 py-1 text-sm font-medium text-indigo-600 hover:bg-indigo-50"
                >
                  Split across categories
                </button>
              </div>
            ) : (
              <fieldset>
                <legend className="text-sm font-medium text-gray-700">Splits</legend>
                <div className="mt-1 space-y-2">
                  {splits.map((s, i) => (
                    <div key={i} className="flex items-start gap-2">
                      <select
                        aria-label={`Split ${i + 1} category`}
                        value={s.categoryId}
                        onChange={(e) =>
                          setSplits(
                            splits.map((row, j) =>
                              j === i ? { ...row, categoryId: e.target.value } : row,
                            ),
                          )
                        }
                        className="block w-full rounded-md border border-gray-300 bg-white px-2 py-1.5 text-sm"
                      >
                        <option value="">Category…</option>
                        {categoryOptions.map((o) => (
                          <option key={o.id} value={o.id}>
                            {o.label}
                          </option>
                        ))}
                      </select>
                      <input
                        aria-label={`Split ${i + 1} amount`}
                        inputMode="decimal"
                        placeholder="0.00"
                        value={s.amount}
                        onChange={(e) =>
                          setSplits(
                            splits.map((row, j) =>
                              j === i ? { ...row, amount: e.target.value } : row,
                            ),
                          )
                        }
                        className="block w-28 rounded-md border border-gray-300 px-2 py-1.5 text-right text-sm tabular-nums"
                      />
                      <input
                        aria-label={`Split ${i + 1} memo`}
                        type="text"
                        placeholder="Memo"
                        value={s.memo}
                        onChange={(e) =>
                          setSplits(
                            splits.map((row, j) =>
                              j === i ? { ...row, memo: e.target.value } : row,
                            ),
                          )
                        }
                        className="block w-28 rounded-md border border-gray-300 px-2 py-1.5 text-sm"
                      />
                      <button
                        type="button"
                        aria-label={`Remove split ${i + 1}`}
                        disabled={splits.length <= 2}
                        onClick={() => setSplits(splits.filter((_, j) => j !== i))}
                        className="rounded-md px-2 py-1.5 text-sm text-gray-500 hover:bg-gray-100 disabled:opacity-40"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>
                <p data-testid="split-remainder" className="mt-2 text-sm text-gray-600">
                  Left to assign:{' '}
                  <span className="tabular-nums">{formatMoney(remainder, currency)}</span>
                </p>
                <div className="mt-1 flex gap-2">
                  <button
                    type="button"
                    onClick={() => setSplits([...splits, { categoryId: '', amount: '', memo: '' }])}
                    className="rounded-md px-2 py-1 text-sm font-medium text-indigo-600 hover:bg-indigo-50"
                  >
                    Add split
                  </button>
                  <button
                    type="button"
                    onClick={() => setSplits([])}
                    className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
                  >
                    Don't split
                  </button>
                </div>
              </fieldset>
            )}

            <div>
              <label htmlFor="tx-tags" className={labelClass}>
                Tags (comma separated)
              </label>
              <input
                id="tx-tags"
                type="text"
                placeholder="vacation, reimbursable"
                value={tagsInput}
                onChange={(e) => setTagsInput(e.target.value)}
                className={inputClass}
              />
            </div>
          </>
        ) : null}

        <div>
          <label htmlFor="tx-notes" className={labelClass}>
            Notes
          </label>
          <input
            id="tx-notes"
            type="text"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            className={inputClass}
          />
        </div>

        {error ? (
          <p role="alert" className="text-sm text-red-600">
            {error}
          </p>
        ) : null}

        <div className="flex items-center gap-2">
          <button
            type="submit"
            disabled={pending}
            className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            {pending ? 'Saving…' : 'Save'}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
          >
            Cancel
          </button>
          {editing ? (
            <button
              type="button"
              disabled={pending}
              onClick={deleteTransaction}
              className="ml-auto rounded-md px-3 py-2 text-sm font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
            >
              Delete
            </button>
          ) : null}
        </div>
      </form>
    </ModalDialog>
  )
}
