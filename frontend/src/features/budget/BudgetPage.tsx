import { useState, type KeyboardEvent } from 'react'
import { categoryIcon } from '../categories/icons'
import type { AllocationDetail, BudgetPeriodDetail } from '../../api/budgets'
import type { Category, CategoryGroup } from '../../api/categories'
import { EmptyState } from '../../components/EmptyState'
import { useIsDesktop } from '../../hooks/useMediaQuery'
import { formatMoney, minorToInputString, parseMoneyInput } from '../../lib/money'
import { useCategoryGroups } from '../categories/useCategories'
import { MoveMoneyDialog } from './MoveMoneyDialog'
import { StatusBadge } from './StatusBadge'
import { useCreatePeriod, usePeriod, useSetAllocation, useUpdatePeriod } from './useBudget'

interface YearMonth {
  year: number
  month: number
}

function shiftMonth({ year, month }: YearMonth, delta: number): YearMonth {
  const index = year * 12 + (month - 1) + delta
  return { year: Math.floor(index / 12), month: (index % 12) + 1 }
}

function monthLabel({ year, month }: YearMonth): string {
  return new Date(Date.UTC(year, month - 1, 1)).toLocaleDateString('en-US', {
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  })
}

// One category row's data: the category plus its allocation, if any exists
// for the period. Categories without an allocation show dashes until funded.
interface RowData {
  category: Category
  allocation: AllocationDetail | undefined
}

interface GroupRows {
  group: CategoryGroup
  rows: RowData[]
}

function buildGroupRows(groups: CategoryGroup[], period: BudgetPeriodDetail): GroupRows[] {
  const byCategory = new Map(period.categories.map((c) => [c.categoryId, c]))
  return groups
    .filter((g) => !g.archivedAt)
    .map((g) => ({
      group: g,
      rows: g.categories
        .filter((c) => !c.archivedAt)
        .map((c) => ({ category: c, allocation: byCategory.get(c.id) })),
    }))
    .filter((g) => g.rows.length > 0)
}

// Inline editor for one category's budgeted amount: a button showing the
// formatted value that turns into an input; Enter or blur commits.
function AllocationCell({
  periodId,
  category,
  allocation,
  currency,
}: {
  periodId: string
  category: Category
  allocation: AllocationDetail | undefined
  currency: string
}) {
  const setAllocation = useSetAllocation()
  const amount = allocation?.amount ?? 0
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState('')
  const [invalid, setInvalid] = useState(false)

  const commit = () => {
    const minor = parseMoneyInput(value)
    if (minor === null || minor < 0) {
      setInvalid(true)
      return
    }
    setEditing(false)
    if (minor !== amount) {
      setAllocation.mutate({ periodId, categoryId: category.id, amount: minor })
    }
  }

  if (!editing) {
    return (
      <button
        type="button"
        aria-label={`Edit budgeted amount for ${category.name}`}
        onClick={() => {
          setValue(minorToInputString(amount))
          setInvalid(false)
          setEditing(true)
        }}
        className="rounded-md px-2 py-1 text-right tabular-nums text-gray-900 hover:bg-indigo-50"
      >
        {formatMoney(amount, currency)}
      </button>
    )
  }
  return (
    <input
      aria-label={`Budgeted amount for ${category.name}`}
      aria-invalid={invalid}
      autoFocus
      inputMode="decimal"
      value={value}
      onChange={(e) => {
        setValue(e.target.value)
        setInvalid(false)
      }}
      onBlur={commit}
      onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
          commit()
        } else if (e.key === 'Escape') {
          setEditing(false)
        }
      }}
      className={`w-28 rounded-md border px-2 py-1 text-right text-sm tabular-nums ${
        invalid ? 'border-red-500' : 'border-gray-300'
      }`}
    />
  )
}

// Inline editor for the period's planned income.
function IncomeEditor({ period }: { period: BudgetPeriodDetail }) {
  const update = useUpdatePeriod()
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState('')
  const [invalid, setInvalid] = useState(false)

  const commit = () => {
    const minor = parseMoneyInput(value)
    if (minor === null || minor < 0) {
      setInvalid(true)
      return
    }
    setEditing(false)
    if (minor !== period.plannedIncome) {
      update.mutate({ periodId: period.id, input: { plannedIncome: minor, notes: period.notes } })
    }
  }

  return (
    <div>
      <p className="text-sm font-medium text-gray-600">Total income</p>
      {editing ? (
        <input
          aria-label="Planned income"
          aria-invalid={invalid}
          autoFocus
          inputMode="decimal"
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            setInvalid(false)
          }}
          onBlur={commit}
          onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
            if (e.key === 'Enter') {
              commit()
            } else if (e.key === 'Escape') {
              setEditing(false)
            }
          }}
          className={`mt-1 w-36 rounded-md border px-2 py-1 text-lg font-semibold tabular-nums ${
            invalid ? 'border-red-500' : 'border-gray-300'
          }`}
        />
      ) : (
        <button
          type="button"
          aria-label="Edit planned income"
          onClick={() => {
            setValue(minorToInputString(period.plannedIncome))
            setInvalid(false)
            setEditing(true)
          }}
          className="mt-1 rounded-md text-xl font-semibold tabular-nums text-gray-900 hover:bg-indigo-50"
        >
          {formatMoney(period.plannedIncome, period.currency)}
        </button>
      )}
    </div>
  )
}

// Budget notes with an explicit save; keyed on period id by the caller so the
// draft resets when switching months.
function NotesEditor({ period }: { period: BudgetPeriodDetail }) {
  const update = useUpdatePeriod()
  const [notes, setNotes] = useState(period.notes)

  return (
    <div className="mt-4">
      <label htmlFor="budget-notes" className="block text-sm font-medium text-gray-600">
        Budget notes
      </label>
      <textarea
        id="budget-notes"
        rows={2}
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        placeholder="Anything to remember about this month…"
        className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
      />
      {notes !== period.notes ? (
        <button
          type="button"
          disabled={update.isPending}
          onClick={() =>
            update.mutate({
              periodId: period.id,
              input: { plannedIncome: period.plannedIncome, notes },
            })
          }
          className="mt-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          Save notes
        </button>
      ) : null}
      {update.error ? (
        <p role="alert" className="mt-1 text-sm text-red-600">
          {update.error.message}
        </p>
      ) : null}
    </div>
  )
}

function CoverOverspendingButton({
  row,
  onCover,
}: {
  row: RowData
  onCover: (toId: string, amount: number) => void
}) {
  if (!row.allocation || row.allocation.status !== 'over_budget') {
    return null
  }
  const overspent = -row.allocation.remaining
  return (
    <button
      type="button"
      onClick={() => onCover(row.category.id, overspent)}
      className="rounded-md px-2 py-1 text-xs font-medium text-indigo-600 hover:bg-indigo-50"
    >
      Cover overspending
    </button>
  )
}

function CategoryTable({
  period,
  groupRows,
  onCover,
}: {
  period: BudgetPeriodDetail
  groupRows: GroupRows[]
  onCover: (toId: string, amount: number) => void
}) {
  return (
    <table className="mt-4 w-full rounded-lg bg-white shadow">
      <thead>
        <tr className="border-b border-gray-200 text-left text-xs font-semibold uppercase tracking-wide text-gray-500">
          <th scope="col" className="px-4 py-3">
            Category
          </th>
          <th scope="col" className="px-4 py-3 text-right">
            Budgeted
          </th>
          <th scope="col" className="px-4 py-3 text-right">
            Activity
          </th>
          <th scope="col" className="px-4 py-3 text-right">
            Remaining
          </th>
          <th scope="col" className="px-4 py-3">
            Status
          </th>
        </tr>
      </thead>
      {groupRows.map(({ group, rows }) => (
        <tbody key={group.id}>
          <tr className="bg-gray-50">
            <th
              scope="colgroup"
              colSpan={5}
              className="px-4 py-2 text-left text-sm font-semibold text-gray-700"
            >
              {group.name}
            </th>
          </tr>
          {rows.map((row) => (
            <tr key={row.category.id} className="border-t border-gray-100">
              <td className="px-4 py-2">
                <span aria-hidden="true" className="mr-2">
                  {categoryIcon(row.category.icon)}
                </span>
                {row.category.name}
              </td>
              <td className="px-4 py-2 text-right">
                <AllocationCell
                  periodId={period.id}
                  category={row.category}
                  allocation={row.allocation}
                  currency={period.currency}
                />
              </td>
              <td className="px-4 py-2 text-right tabular-nums text-gray-700">
                {row.allocation ? formatMoney(row.allocation.spending, period.currency) : '—'}
              </td>
              <td className="px-4 py-2 text-right tabular-nums text-gray-700">
                {row.allocation ? formatMoney(row.allocation.remaining, period.currency) : '—'}
              </td>
              <td className="px-4 py-2">
                {row.allocation ? <StatusBadge status={row.allocation.status} /> : null}
                <CoverOverspendingButton row={row} onCover={onCover} />
              </td>
            </tr>
          ))}
        </tbody>
      ))}
    </table>
  )
}

// Mobile layout: the table collapses to stacked cards per category.
function CategoryCards({
  period,
  groupRows,
  onCover,
}: {
  period: BudgetPeriodDetail
  groupRows: GroupRows[]
  onCover: (toId: string, amount: number) => void
}) {
  return (
    <div className="mt-4 space-y-4">
      {groupRows.map(({ group, rows }) => (
        <section key={group.id} aria-label={group.name}>
          <h2 className="text-sm font-semibold text-gray-700">{group.name}</h2>
          <div className="mt-2 space-y-2">
            {rows.map((row) => (
              <div key={row.category.id} className="rounded-lg bg-white p-3 shadow">
                <div className="flex items-center justify-between gap-2">
                  <p className="font-medium text-gray-900">
                    <span aria-hidden="true" className="mr-2">
                      {categoryIcon(row.category.icon)}
                    </span>
                    {row.category.name}
                  </p>
                  {row.allocation ? <StatusBadge status={row.allocation.status} /> : null}
                </div>
                <dl className="mt-2 grid grid-cols-3 gap-2 text-sm">
                  <div>
                    <dt className="text-xs text-gray-500">Budgeted</dt>
                    <dd>
                      <AllocationCell
                        periodId={period.id}
                        category={row.category}
                        allocation={row.allocation}
                        currency={period.currency}
                      />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-gray-500">Activity</dt>
                    <dd className="tabular-nums text-gray-700">
                      {row.allocation ? formatMoney(row.allocation.spending, period.currency) : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-gray-500">Remaining</dt>
                    <dd className="tabular-nums text-gray-700">
                      {row.allocation
                        ? formatMoney(row.allocation.remaining, period.currency)
                        : '—'}
                    </dd>
                  </div>
                </dl>
                <CoverOverspendingButton row={row} onCover={onCover} />
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

export function BudgetPage() {
  const [ym, setYm] = useState<YearMonth>(() => {
    const now = new Date()
    return { year: now.getFullYear(), month: now.getMonth() + 1 }
  })
  const isDesktop = useIsDesktop()
  const { data: period, isLoading, error } = usePeriod(ym.year, ym.month)
  const { data: groups } = useCategoryGroups()
  const create = useCreatePeriod()
  const [moveMoney, setMoveMoney] = useState<{ toId?: string; amount?: number } | null>(null)

  const label = monthLabel(ym)
  const groupRows = period && groups ? buildGroupRows(groups, period) : []
  const openCover = (toId: string, amount: number) => setMoveMoney({ toId, amount })

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold text-gray-900">Budget</h1>
        <div className="flex items-center gap-2">
          <button
            type="button"
            aria-label="Previous month"
            onClick={() => setYm((v) => shiftMonth(v, -1))}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
          >
            ←
          </button>
          <span className="min-w-36 text-center text-sm font-semibold text-gray-900">{label}</span>
          <button
            type="button"
            aria-label="Next month"
            onClick={() => setYm((v) => shiftMonth(v, 1))}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
          >
            →
          </button>
        </div>
      </div>

      {isLoading ? <p className="mt-6 text-sm text-gray-600">Loading…</p> : null}
      {error ? (
        <p role="alert" className="mt-6 text-sm text-red-600">
          {error.message}
        </p>
      ) : null}

      {!isLoading && period === null ? (
        <EmptyState
          title={`No budget for ${label}`}
          description="Create this month's budget to start assigning your income to categories."
        >
          <div className="mt-4 flex justify-center gap-2">
            <button
              type="button"
              disabled={create.isPending}
              onClick={() =>
                create.mutate({
                  year: ym.year,
                  month: ym.month,
                  copyPrior: false,
                  plannedIncome: 0,
                  notes: '',
                })
              }
              className="rounded-md border border-gray-300 px-4 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-100 disabled:opacity-50"
            >
              Start from scratch
            </button>
            <button
              type="button"
              disabled={create.isPending}
              onClick={() =>
                create.mutate({
                  year: ym.year,
                  month: ym.month,
                  copyPrior: true,
                  plannedIncome: 0,
                  notes: '',
                })
              }
              className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              Copy last month
            </button>
          </div>
          {create.error ? (
            <p role="alert" className="mt-2 text-sm text-red-600">
              {create.error.message}
            </p>
          ) : null}
        </EmptyState>
      ) : null}

      {period ? (
        <>
          <div className="mt-4 rounded-lg bg-white p-4 shadow">
            <div className="flex flex-wrap items-start justify-between gap-6">
              <IncomeEditor period={period} />
              <div>
                <p className="text-sm font-medium text-gray-600">Available to assign</p>
                <p
                  data-testid="available-to-assign"
                  className={`mt-1 text-xl font-semibold tabular-nums ${
                    period.unallocated < 0 ? 'text-red-700' : 'text-green-700'
                  }`}
                >
                  {formatMoney(period.unallocated, period.currency)}
                </p>
                <p className="text-xs text-gray-500">
                  {period.unallocated < 0
                    ? 'Over-assigned — you have budgeted more than your available funds.'
                    : 'Income plus rollover not yet assigned to a category.'}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setMoveMoney({})}
                className="rounded-md border border-gray-300 px-4 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-100"
              >
                Move money
              </button>
            </div>
            <NotesEditor key={period.id} period={period} />
          </div>

          {groups && groupRows.length === 0 ? (
            <p className="mt-6 text-sm text-gray-600">
              No categories yet — create categories first, then assign money to them here.
            </p>
          ) : isDesktop ? (
            <CategoryTable period={period} groupRows={groupRows} onCover={openCover} />
          ) : (
            <CategoryCards period={period} groupRows={groupRows} onCover={openCover} />
          )}

          {moveMoney ? (
            <MoveMoneyDialog
              period={period}
              groups={groups ?? []}
              initialToId={moveMoney.toId}
              initialAmount={moveMoney.amount}
              onClose={() => setMoveMoney(null)}
            />
          ) : null}
        </>
      ) : null}
    </>
  )
}
