import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import type { Account } from '../../api/accounts'
import { formatMoney, parseMoneyInput } from '../../lib/money'
import { AccountForm } from './AccountForm'
import { accountTypeLabels } from './labels'
import { useAccount, useArchiveAccount, useReconcileAccount, useUpdateAccount } from './useAccounts'

function ReconcileSection({ account }: { account: Account }) {
  const reconcile = useReconcileAccount(account.id)
  const [statement, setStatement] = useState('')
  const [inputError, setInputError] = useState<string | null>(null)

  const submit = () => {
    const minor = parseMoneyInput(statement)
    if (minor === null) {
      setInputError('Enter an amount like 1250.00')
      return
    }
    setInputError(null)
    reconcile.mutate(minor)
  }

  return (
    <section aria-label="Reconcile" className="mt-8 max-w-lg rounded-lg bg-white p-6 shadow">
      <h2 className="text-lg font-semibold text-gray-900">Reconcile</h2>
      <p className="mt-1 text-sm text-gray-600">
        Enter the balance from your latest statement. If it differs from the current balance of{' '}
        {formatMoney(account.balance, account.currency)}, BudgetFlow records an adjustment
        transaction for the difference.
      </p>
      <div className="mt-4 flex items-end gap-2">
        <div className="flex-1">
          <label htmlFor="statement-balance" className="block text-sm font-medium text-gray-700">
            Statement balance
          </label>
          <input
            id="statement-balance"
            type="text"
            inputMode="decimal"
            value={statement}
            onChange={(e) => setStatement(e.target.value)}
            aria-invalid={inputError ? true : undefined}
            aria-describedby={inputError ? 'statement-balance-error' : undefined}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500 focus:outline-none focus-visible:ring-2"
          />
        </div>
        <button
          type="button"
          onClick={submit}
          disabled={reconcile.isPending}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          Reconcile
        </button>
      </div>
      {inputError ? (
        <p id="statement-balance-error" role="alert" className="mt-1 text-sm text-red-600">
          {inputError}
        </p>
      ) : null}
      {reconcile.error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {reconcile.error.message}
        </p>
      ) : null}
      {reconcile.isSuccess ? (
        <p role="status" className="mt-2 rounded-md bg-green-50 px-3 py-2 text-sm text-green-700">
          {reconcile.data.adjustment
            ? `Adjustment of ${formatMoney(reconcile.data.adjustment.amount, account.currency)} recorded. New balance: ${formatMoney(reconcile.data.account.balance, account.currency)}.`
            : 'Balances already match — no adjustment needed.'}
        </p>
      ) : null}
    </section>
  )
}

export function AccountDetailPage() {
  const { accountId = '' } = useParams()
  const { data: account, isLoading, error } = useAccount(accountId)
  const update = useUpdateAccount(accountId)
  const archive = useArchiveAccount(accountId)

  if (isLoading) {
    return <p className="text-sm text-gray-600">Loading…</p>
  }
  if (error || !account) {
    return (
      <>
        <p role="alert" className="text-sm text-red-600">
          {error?.message ?? 'Account not found'}
        </p>
        <Link to="/accounts" className="mt-2 inline-block text-sm text-indigo-600">
          Back to accounts
        </Link>
      </>
    )
  }

  const archived = Boolean(account.archivedAt)

  return (
    <>
      <Link to="/accounts" className="text-sm text-indigo-600">
        ← Accounts
      </Link>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">{account.name}</h1>
          <p className="text-sm text-gray-600">
            {accountTypeLabels[account.type]}
            {account.institution ? ` · ${account.institution}` : ''}
            {archived ? ' · Archived' : ''}
          </p>
        </div>
        <p className="text-2xl font-semibold text-gray-900">
          {formatMoney(account.balance, account.currency)}
        </p>
      </div>

      <section aria-label="Edit account" className="mt-6 max-w-lg rounded-lg bg-white p-6 shadow">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">Details</h2>
        <AccountForm
          defaults={account}
          submitLabel="Save changes"
          pending={update.isPending}
          errorMessage={update.error?.message}
          onSubmit={(input) => update.mutate(input)}
        />
        {update.isSuccess ? (
          <p role="status" className="mt-3 rounded-md bg-green-50 px-3 py-2 text-sm text-green-700">
            Account saved.
          </p>
        ) : null}
      </section>

      <ReconcileSection account={account} />

      <section aria-label="Archive" className="mt-8 max-w-lg">
        <button
          type="button"
          onClick={() => archive.mutate(!archived)}
          disabled={archive.isPending}
          className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
        >
          {archived ? 'Unarchive account' : 'Archive account'}
        </button>
        <p className="mt-1 text-sm text-gray-600">
          Archiving hides the account from pickers; its transaction history is kept.
        </p>
      </section>
    </>
  )
}
