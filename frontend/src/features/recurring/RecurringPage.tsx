import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { Account } from '../../api/accounts'
import { listCategoryGroups, type CategoryGroup } from '../../api/categories'
import { listTransactions } from '../../api/transactions'
import type { RecurringRule } from '../../api/recurring'
import { frequencyLabels } from '../../api/recurring'
import { EmptyState } from '../../components/EmptyState'
import { formatDate } from '../../lib/dates'
import { formatMoney } from '../../lib/money'
import { useAccounts } from '../accounts/useAccounts'
import { RecurringRuleForm } from './RecurringRuleForm'
import {
  useArchiveRecurringRule,
  useCreateRecurringRule,
  useMarkRulePaid,
  useMatchRule,
  useRecurringRules,
  useUpdateRecurringRule,
} from './useRecurring'

const actionButtonClass =
  'rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500'

// Lets the user link an existing transaction on the rule's account as this
// occurrence's payment instead of creating a new one.
function MatchTransactionPanel({
  rule,
  currency,
  onClose,
}: {
  rule: RecurringRule
  currency: string
  onClose: () => void
}) {
  const match = useMatchRule()
  const { data: page, isLoading } = useQuery({
    queryKey: ['transactions', `accountId=${rule.accountId}&limit=10`],
    queryFn: ({ signal }) => listTransactions({ accountId: rule.accountId, limit: 10 }, signal),
  })
  const candidates = (page?.transactions ?? []).filter((t) => t.type === 'expense')

  return (
    <section
      aria-label={`Match a transaction to ${rule.name}`}
      className="mt-2 rounded-md border border-indigo-200 bg-indigo-50 p-3"
    >
      <p className="text-sm font-medium text-gray-900">
        Match a recent transaction to {rule.name}
      </p>
      {isLoading ? <p className="mt-2 text-sm text-gray-600">Loading transactions…</p> : null}
      {!isLoading && candidates.length === 0 ? (
        <p className="mt-2 text-sm text-gray-600">
          No recent expenses on this account to match. Mark the bill paid instead to create one.
        </p>
      ) : null}
      {match.error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {match.error.message}
        </p>
      ) : null}
      <ul className="mt-2 space-y-1">
        {candidates.map((t) => (
          <li
            key={t.id}
            className="flex items-center justify-between gap-2 rounded bg-white px-3 py-2 text-sm"
          >
            <span>
              {formatDate(t.date)} · {t.payee || '(no payee)'} ·{' '}
              {formatMoney(t.amount, currency)}
            </span>
            <button
              type="button"
              disabled={match.isPending}
              onClick={() =>
                match.mutate({ id: rule.id, transactionId: t.id }, { onSuccess: onClose })
              }
              className={actionButtonClass}
            >
              Match
            </button>
          </li>
        ))}
      </ul>
      <button type="button" onClick={onClose} className={`mt-2 ${actionButtonClass}`}>
        Cancel
      </button>
    </section>
  )
}

function EditRuleSection({
  rule,
  accounts,
  groups,
  onClose,
}: {
  rule: RecurringRule
  accounts: Account[]
  groups: CategoryGroup[]
  onClose: () => void
}) {
  const update = useUpdateRecurringRule(rule.id)
  return (
    <section aria-label={`Edit ${rule.name}`}>
      <h2 className="mb-4 text-lg font-semibold text-gray-900">Edit {rule.name}</h2>
      <RecurringRuleForm
        accounts={accounts}
        groups={groups}
        rule={rule}
        submitLabel="Save changes"
        pending={update.isPending}
        errorMessage={update.error?.message}
        onCancel={onClose}
        onSubmit={(input) => update.mutate(input, { onSuccess: onClose })}
      />
    </section>
  )
}

export function RecurringPage() {
  const { data: rules, isLoading } = useRecurringRules()
  const { data: accounts } = useAccounts()
  const { data: groups } = useQuery({
    queryKey: ['categories'],
    queryFn: ({ signal }) => listCategoryGroups(signal),
  })

  const create = useCreateRecurringRule()
  const archive = useArchiveRecurringRule()
  const markPaid = useMarkRulePaid()

  const [showCreate, setShowCreate] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [matchingId, setMatchingId] = useState<string | null>(null)

  const accountById = useMemo(
    () => new Map((accounts ?? []).map((a) => [a.id, a])),
    [accounts],
  )
  const categoryById = useMemo(
    () => new Map((groups ?? []).flatMap((g) => g.categories.map((c) => [c.id, c] as const))),
    [groups],
  )

  const active = (rules ?? [])
    .filter((r) => !r.archived)
    .sort((a, b) => a.nextDueDate.localeCompare(b.nextDueDate))
  const archived = (rules ?? []).filter((r) => r.archived)

  const currencyFor = (rule: RecurringRule) => accountById.get(rule.accountId)?.currency ?? 'USD'

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Recurring</h1>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2"
        >
          Add rule
        </button>
      </div>

      {showCreate ? (
        <section aria-label="New recurring rule" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">New recurring rule</h2>
          <RecurringRuleForm
            accounts={accounts ?? []}
            groups={groups ?? []}
            submitLabel="Create rule"
            pending={create.isPending}
            errorMessage={create.error?.message}
            onCancel={() => setShowCreate(false)}
            onSubmit={(input) => create.mutate(input, { onSuccess: () => setShowCreate(false) })}
          />
        </section>
      ) : null}

      {!isLoading && rules && rules.length === 0 && !showCreate ? (
        <EmptyState
          title="No recurring bills yet"
          description="Track rent, subscriptions, and other repeating payments to get reminders before they are due."
        >
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            className="mt-4 inline-block rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
          >
            Add your first rule
          </button>
        </EmptyState>
      ) : null}

      {markPaid.error ? (
        <p role="alert" className="mt-4 text-sm text-red-600">
          {markPaid.error.message}
        </p>
      ) : null}

      {active.length > 0 ? (
        <ul className="mt-6 max-w-3xl space-y-2">
          {active.map((rule) => (
            <li key={rule.id} className="rounded-lg bg-white px-4 py-3 shadow">
              {editingId === rule.id ? (
                <EditRuleSection
                  rule={rule}
                  accounts={accounts ?? []}
                  groups={groups ?? []}
                  onClose={() => setEditingId(null)}
                />
              ) : (
                <>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p className="font-medium text-gray-900">{rule.name}</p>
                      <p className="text-sm text-gray-600">
                        {categoryById.get(rule.categoryId)?.name ?? 'Category'} ·{' '}
                        {accountById.get(rule.accountId)?.name ?? 'Account'} ·{' '}
                        {frequencyLabels[rule.frequency]}
                        {rule.frequency === 'custom' && rule.customIntervalDays
                          ? ` (every ${rule.customIntervalDays} days)`
                          : ''}
                      </p>
                    </div>
                    <div className="text-right">
                      <p className="font-semibold text-gray-900">
                        {formatMoney(rule.amount, currencyFor(rule))}
                      </p>
                      <p className="text-sm text-gray-600">Due {formatDate(rule.nextDueDate)}</p>
                    </div>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={markPaid.isPending}
                      onClick={() => markPaid.mutate({ id: rule.id })}
                      className={actionButtonClass}
                    >
                      Mark paid
                    </button>
                    <button
                      type="button"
                      onClick={() => setMatchingId(matchingId === rule.id ? null : rule.id)}
                      className={actionButtonClass}
                    >
                      Match transaction
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditingId(rule.id)}
                      className={actionButtonClass}
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      onClick={() => archive.mutate({ id: rule.id, archived: true })}
                      className={actionButtonClass}
                    >
                      Archive
                    </button>
                  </div>
                  {matchingId === rule.id ? (
                    <MatchTransactionPanel
                      rule={rule}
                      currency={currencyFor(rule)}
                      onClose={() => setMatchingId(null)}
                    />
                  ) : null}
                </>
              )}
            </li>
          ))}
        </ul>
      ) : null}

      {archived.length > 0 ? (
        <section className="mt-8 max-w-3xl">
          <h2 className="text-lg font-semibold text-gray-900">Archived</h2>
          <ul className="mt-2 space-y-2 opacity-70">
            {archived.map((rule) => (
              <li
                key={rule.id}
                className="flex items-center justify-between rounded-lg bg-white px-4 py-3 shadow"
              >
                <p className="font-medium text-gray-900">{rule.name}</p>
                <button
                  type="button"
                  onClick={() => archive.mutate({ id: rule.id, archived: false })}
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
