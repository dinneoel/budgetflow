import { EmptyState } from '../../components/EmptyState'

export function ReportsPage() {
  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Reports</h1>
      <EmptyState
        title="Nothing to report yet"
        description="Spending by category, monthly trends, cash flow, and net worth will appear here once you have transactions."
        actionLabel="Go to transactions"
        actionTo="/transactions"
      />
    </>
  )
}
