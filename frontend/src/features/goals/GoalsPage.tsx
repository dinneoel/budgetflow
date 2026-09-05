import { EmptyState } from '../../components/EmptyState'

export function GoalsPage() {
  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Goals</h1>
      <EmptyState
        title="No goals yet"
        description="Set a savings, payoff, or purchase goal and BudgetFlow will tell you the monthly contribution needed to stay on schedule."
      />
    </>
  )
}
