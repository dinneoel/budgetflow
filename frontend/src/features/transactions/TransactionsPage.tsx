import { EmptyState } from '../../components/EmptyState'

export function TransactionsPage() {
  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Transactions</h1>
      <EmptyState
        title="No transactions yet"
        description="Record income, expenses, and transfers here — or import a CSV from your bank to get started quickly."
      />
    </>
  )
}
