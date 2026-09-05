import { useState, type FormEvent } from 'react'
import type { BudgetPeriodDetail } from '../../api/budgets'
import { ModalDialog } from '../../components/ModalDialog'
import type { CategoryGroup } from '../../api/categories'
import { formatMoney, minorToInputString, parseMoneyInput } from '../../lib/money'
import { useSetAllocation } from './useBudget'

// Moves budgeted funds from one category to another as two allocation writes:
// the source is decreased first, then the target is increased using the fresh
// amounts from the first response.
export function MoveMoneyDialog({
  period,
  groups,
  initialToId = '',
  initialAmount,
  onClose,
}: {
  period: BudgetPeriodDetail
  groups: CategoryGroup[]
  initialToId?: string
  initialAmount?: number
  onClose: () => void
}) {
  const setAllocation = useSetAllocation()
  const allocations = new Map(period.categories.map((c) => [c.categoryId, c]))
  const options = groups.flatMap((g) =>
    g.categories
      .filter((c) => !c.archivedAt)
      .map((c) => ({ id: c.id, label: `${g.name} · ${c.name}` })),
  )
  const [fromId, setFromId] = useState('')
  const [toId, setToId] = useState(initialToId)
  const [amountInput, setAmountInput] = useState(
    initialAmount !== undefined ? minorToInputString(initialAmount) : '',
  )
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const fromBudgeted = allocations.get(fromId)?.amount ?? 0

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const amount = parseMoneyInput(amountInput)
    if (!fromId || !toId) {
      setError('Choose both categories.')
      return
    }
    if (fromId === toId) {
      setError('Choose two different categories.')
      return
    }
    if (amount === null || amount <= 0) {
      setError('Enter a positive amount.')
      return
    }
    if (amount > fromBudgeted) {
      setError(
        `You can move at most ${formatMoney(fromBudgeted, period.currency)} from that category.`,
      )
      return
    }
    setError(null)
    setPending(true)
    try {
      const afterFrom = await setAllocation.mutateAsync({
        periodId: period.id,
        categoryId: fromId,
        amount: fromBudgeted - amount,
      })
      const toBudgeted = afterFrom.categories.find((c) => c.categoryId === toId)?.amount ?? 0
      await setAllocation.mutateAsync({
        periodId: period.id,
        categoryId: toId,
        amount: toBudgeted + amount,
      })
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Moving money failed.')
    } finally {
      setPending(false)
    }
  }

  return (
    <ModalDialog label="Move money" onClose={onClose} className="max-w-md">
      <h2 className="text-lg font-semibold text-gray-900">Move money</h2>
      <form onSubmit={submit} className="mt-3 space-y-3">
        <div>
          <label htmlFor="move-from" className="block text-sm font-medium text-gray-700">
            Move from
          </label>
          <select
            id="move-from"
            value={fromId}
            onChange={(e) => setFromId(e.target.value)}
            className="mt-1 block w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm"
          >
            <option value="">Choose a category…</option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
          {fromId ? (
            <p className="mt-1 text-xs text-gray-600">
              Budgeted: {formatMoney(fromBudgeted, period.currency)}
            </p>
          ) : null}
        </div>
        <div>
          <label htmlFor="move-to" className="block text-sm font-medium text-gray-700">
            Move to
          </label>
          <select
            id="move-to"
            value={toId}
            onChange={(e) => setToId(e.target.value)}
            className="mt-1 block w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm"
          >
            <option value="">Choose a category…</option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="move-amount" className="block text-sm font-medium text-gray-700">
            Amount
          </label>
          <input
            id="move-amount"
            inputMode="decimal"
            value={amountInput}
            onChange={(e) => setAmountInput(e.target.value)}
            placeholder="0.00"
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
          />
        </div>
        {error ? (
          <p role="alert" className="text-sm text-red-600">
            {error}
          </p>
        ) : null}
        <div className="flex gap-2">
          <button
            type="submit"
            disabled={pending}
            className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            {pending ? 'Moving…' : 'Move'}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
          >
            Cancel
          </button>
        </div>
      </form>
    </ModalDialog>
  )
}
