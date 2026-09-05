import { EmptyState } from '../../components/EmptyState'

export function BudgetPage() {
  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Budget</h1>
      <EmptyState
        title="No budget yet"
        description="Create a monthly budget to allocate your income across categories and track what's left to assign."
      />
    </>
  )
}
