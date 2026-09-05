import type { BudgetStatus } from '../../api/budgets'

// Status is always conveyed by icon + text, never color alone.
const statusMeta: Record<BudgetStatus, { label: string; icon: string; className: string }> = {
  on_track: { label: 'On track', icon: '✓', className: 'bg-green-100 text-green-800' },
  approaching_limit: { label: 'Approaching limit', icon: '⚠', className: 'bg-amber-100 text-amber-900' },
  over_budget: { label: 'Over budget', icon: '✕', className: 'bg-red-100 text-red-800' },
  unfunded: { label: 'Unfunded', icon: '○', className: 'bg-gray-200 text-gray-700' },
}

export function StatusBadge({ status }: { status: BudgetStatus }) {
  const meta = statusMeta[status]
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${meta.className}`}
    >
      <span aria-hidden="true">{meta.icon}</span>
      {meta.label}
    </span>
  )
}
