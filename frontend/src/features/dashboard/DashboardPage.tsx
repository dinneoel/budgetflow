import { EmptyState } from '../../components/EmptyState'

export function DashboardPage() {
  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
      <EmptyState
        title="Welcome to BudgetFlow"
        description="Your dashboard will show balances, monthly spending, budget health, upcoming bills, and goals once you add an account and a few transactions."
        actionLabel="Add your first transaction"
        actionTo="/transactions?quick-add=1"
      />
    </>
  )
}
