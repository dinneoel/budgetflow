import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { listCategoryGroups } from '../../api/categories'
import type { Account } from '../../api/accounts'
import type { CategoryGroup } from '../../api/categories'
import { goalTypeLabels, type Goal } from '../../api/goals'
import { EmptyState } from '../../components/EmptyState'
import { Field } from '../../components/forms/Field'
import { formatDate, todayISO } from '../../lib/dates'
import { formatMoney, parseMoneyInput } from '../../lib/money'
import { useAccounts } from '../accounts/useAccounts'
import { useCurrentUser } from '../auth/useAuth'
import { GoalForm } from './GoalForm'
import { useAddContribution, useArchiveGoal, useCreateGoal, useGoals, useUpdateGoal } from './useGoals'

const actionButtonClass =
  'rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500'

// Inline amount + date form recording a manual contribution against a goal.
function ContributionForm({ goal, onDone }: { goal: Goal; onDone: () => void }) {
  const add = useAddContribution(goal.id)
  const [amount, setAmount] = useState('')
  const [date, setDate] = useState(todayISO())
  const [error, setError] = useState<string | null>(null)

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const minor = parseMoneyInput(amount)
    if (minor === null || minor === 0) {
      setError('Enter an amount like 1250.00')
      return
    }
    setError(null)
    add.mutate({ amount: minor, date, notes: '' }, { onSuccess: onDone })
  }

  return (
    <form onSubmit={submit} noValidate className="mt-3 space-y-2 rounded-md bg-gray-50 p-3">
      <Field
        label="Contribution amount"
        type="text"
        inputMode="decimal"
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
        error={error ?? add.error?.message}
      />
      <Field
        label="Contribution date"
        type="date"
        value={date}
        onChange={(e) => setDate(e.target.value)}
      />
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={add.isPending}
          className="rounded-md bg-indigo-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          {add.isPending ? 'Saving…' : 'Record contribution'}
        </button>
        <button type="button" onClick={onDone} className={actionButtonClass}>
          Cancel
        </button>
      </div>
    </form>
  )
}

function GoalCard({
  goal,
  currency,
  accounts,
  groups,
}: {
  goal: Goal
  currency: string
  accounts: Account[]
  groups: CategoryGroup[]
}) {
  const archive = useArchiveGoal()
  const update = useUpdateGoal(goal.id)
  const [editing, setEditing] = useState(false)
  const [contributing, setContributing] = useState(false)

  const pct =
    goal.targetAmount > 0
      ? Math.max(0, Math.min(100, Math.round((goal.currentBalance / goal.targetAmount) * 100)))
      : 0

  if (editing) {
    return (
      <section aria-label={`Edit ${goal.name}`} className="rounded-lg bg-white p-4 shadow">
        <h3 className="mb-3 text-lg font-semibold text-gray-900">Edit {goal.name}</h3>
        <GoalForm
          accounts={accounts}
          groups={groups}
          goal={goal}
          submitLabel="Save changes"
          pending={update.isPending}
          errorMessage={update.error?.message}
          onCancel={() => setEditing(false)}
          onSubmit={(input) => update.mutate(input, { onSuccess: () => setEditing(false) })}
        />
      </section>
    )
  }

  return (
    <section aria-label={goal.name} className="rounded-lg bg-white p-4 shadow">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="font-semibold text-gray-900">{goal.name}</h3>
          <p className="text-sm text-gray-600">
            {goalTypeLabels[goal.type]}
            {goal.targetDate ? ` · by ${formatDate(goal.targetDate)}` : ''}
          </p>
        </div>
        {goal.behindSchedule ? (
          <p className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800">
            <span aria-hidden="true">⚠ </span>Behind schedule
          </p>
        ) : (
          <p className="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-800">
            <span aria-hidden="true">✓ </span>On track
          </p>
        )}
      </div>

      <div
        role="progressbar"
        aria-label={`${goal.name} progress`}
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuetext={`${pct}% of target saved`}
        className="mt-3 h-2 overflow-hidden rounded-full bg-gray-200"
      >
        <div className="h-full rounded-full bg-indigo-600" style={{ width: `${pct}%` }} />
      </div>
      <p className="mt-2 text-sm text-gray-700">
        {formatMoney(goal.currentBalance, currency)} of {formatMoney(goal.targetAmount, currency)}{' '}
        ({pct}%)
      </p>
      {goal.targetDate && goal.amountRemaining > 0 ? (
        <p className="text-sm text-gray-600">
          Contribute {formatMoney(goal.requiredMonthlyContribution, currency)}/month to stay on
          schedule.
        </p>
      ) : null}

      <div className="mt-3 flex flex-wrap gap-2">
        <button type="button" onClick={() => setContributing(true)} className={actionButtonClass}>
          Add contribution
        </button>
        <button type="button" onClick={() => setEditing(true)} className={actionButtonClass}>
          Edit
        </button>
        <button
          type="button"
          onClick={() => archive.mutate({ id: goal.id, archived: true })}
          className={actionButtonClass}
        >
          Archive
        </button>
      </div>
      {contributing ? <ContributionForm goal={goal} onDone={() => setContributing(false)} /> : null}
    </section>
  )
}

export function GoalsPage() {
  const { data: goals, isLoading } = useGoals()
  const { data: accounts } = useAccounts()
  const { data: user } = useCurrentUser()
  const { data: groups } = useQuery({
    queryKey: ['categories'],
    queryFn: ({ signal }) => listCategoryGroups(signal),
  })
  const create = useCreateGoal()
  const archive = useArchiveGoal()
  const [showCreate, setShowCreate] = useState(false)

  const currency = user?.defaultCurrency ?? 'USD'
  const active = (goals ?? []).filter((g) => !g.archived)
  const archived = (goals ?? []).filter((g) => g.archived)

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Goals</h1>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2"
        >
          Add goal
        </button>
      </div>

      {showCreate ? (
        <section aria-label="New goal" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">New goal</h2>
          <GoalForm
            accounts={accounts ?? []}
            groups={groups ?? []}
            submitLabel="Create goal"
            pending={create.isPending}
            errorMessage={create.error?.message}
            onCancel={() => setShowCreate(false)}
            onSubmit={(input) => create.mutate(input, { onSuccess: () => setShowCreate(false) })}
          />
        </section>
      ) : null}

      {!isLoading && goals && goals.length === 0 && !showCreate ? (
        <EmptyState
          title="No goals yet"
          description="Set a savings, payoff, or purchase goal and BudgetFlow will tell you the monthly contribution needed to stay on schedule."
        >
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            className="mt-4 inline-block rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
          >
            Add your first goal
          </button>
        </EmptyState>
      ) : null}

      {active.length > 0 ? (
        <div className="mt-6 grid max-w-4xl gap-4 md:grid-cols-2">
          {active.map((g) => (
            <GoalCard
              key={g.id}
              goal={g}
              currency={currency}
              accounts={accounts ?? []}
              groups={groups ?? []}
            />
          ))}
        </div>
      ) : null}

      {archived.length > 0 ? (
        <section className="mt-8 max-w-4xl">
          <h2 className="text-lg font-semibold text-gray-900">Archived</h2>
          <ul className="mt-2 space-y-2 opacity-70">
            {archived.map((g) => (
              <li
                key={g.id}
                className="flex items-center justify-between rounded-lg bg-white px-4 py-3 shadow"
              >
                <p className="font-medium text-gray-900">{g.name}</p>
                <button
                  type="button"
                  onClick={() => archive.mutate({ id: g.id, archived: false })}
                  className={actionButtonClass}
                >
                  Unarchive
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </>
  )
}
