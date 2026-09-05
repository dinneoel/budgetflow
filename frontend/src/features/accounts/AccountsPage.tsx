import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { Account } from '../../api/accounts'
import { EmptyState } from '../../components/EmptyState'
import { formatMoney } from '../../lib/money'
import { AccountForm } from './AccountForm'
import { accountTypeLabels } from './labels'
import { useAccounts, useCreateAccount } from './useAccounts'

function AccountRow({ account }: { account: Account }) {
  return (
    <li>
      <Link
        to={`/accounts/${account.id}`}
        className="flex items-center justify-between rounded-lg bg-white px-4 py-3 shadow hover:bg-gray-50"
      >
        <div>
          <p className="font-medium text-gray-900">{account.name}</p>
          <p className="text-sm text-gray-600">
            {accountTypeLabels[account.type]}
            {account.institution ? ` · ${account.institution}` : ''}
            {account.archivedAt ? ' · Archived' : ''}
          </p>
        </div>
        <p className="text-right font-semibold text-gray-900">
          {formatMoney(account.balance, account.currency)}
        </p>
      </Link>
    </li>
  )
}

export function AccountsPage() {
  const { data: accounts, isLoading } = useAccounts()
  const create = useCreateAccount()
  const [showForm, setShowForm] = useState(false)

  const active = (accounts ?? []).filter((a) => !a.archivedAt)
  const archived = (accounts ?? []).filter((a) => a.archivedAt)

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Accounts</h1>
        <button
          type="button"
          onClick={() => setShowForm(true)}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2"
        >
          Add account
        </button>
      </div>

      {showForm ? (
        <section aria-label="New account" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">New account</h2>
          <AccountForm
            submitLabel="Create account"
            pending={create.isPending}
            errorMessage={create.error?.message}
            onCancel={() => setShowForm(false)}
            onSubmit={(input) =>
              create.mutate(input, { onSuccess: () => setShowForm(false) })
            }
          />
        </section>
      ) : null}

      {!isLoading && accounts && accounts.length === 0 && !showForm ? (
        <EmptyState
          title="No accounts yet"
          description="Add your cash, bank, and card accounts to start tracking balances and transactions."
        >
          <button
            type="button"
            onClick={() => setShowForm(true)}
            className="mt-4 inline-block rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
          >
            Add your first account
          </button>
        </EmptyState>
      ) : null}

      {active.length > 0 ? (
        <ul className="mt-6 max-w-2xl space-y-2">
          {active.map((a) => (
            <AccountRow key={a.id} account={a} />
          ))}
        </ul>
      ) : null}

      {archived.length > 0 ? (
        <section className="mt-8 max-w-2xl">
          <h2 className="text-lg font-semibold text-gray-900">Archived</h2>
          <ul className="mt-2 space-y-2 opacity-70">
            {archived.map((a) => (
              <AccountRow key={a.id} account={a} />
            ))}
          </ul>
        </section>
      ) : null}
    </>
  )
}
