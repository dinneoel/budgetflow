import { Link } from 'react-router-dom'
import type { BudgetHealth, Dashboard } from '../../api/reports'
import { EmptyState } from '../../components/EmptyState'
import { formatDate } from '../../lib/dates'
import { formatMoney } from '../../lib/money'
import { StatusBadge } from '../budget/StatusBadge'
import { useDashboard } from './useDashboard'

// Health is always conveyed by icon + text, never color alone.
const healthMeta: Record<BudgetHealth, { label: string; icon: string; className: string }> = {
  no_budget: {
    label: 'No budget for this month yet',
    icon: '○',
    className: 'bg-gray-200 text-gray-700',
  },
  on_track: { label: 'Budget on track', icon: '✓', className: 'bg-green-100 text-green-800' },
  approaching_limit: {
    label: 'Some categories approaching their limit',
    icon: '⚠',
    className: 'bg-amber-100 text-amber-900',
  },
  over_budget: {
    label: 'Over budget in some categories',
    icon: '✕',
    className: 'bg-red-100 text-red-800',
  },
}

const quickActions = [
  { to: '/transactions?quick-add=1', label: 'Add transaction' },
  { to: '/transactions?quick-add=income', label: 'Add income' },
  { to: '/transactions?quick-add=transfer', label: 'Transfer' },
  { to: '/categories', label: 'Create category' },
  { to: '/budget', label: 'Allocate funds' },
]

function SummaryCard({ title, value, hint }: { title: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg bg-white p-4 shadow">
      <p className="text-sm text-gray-600">{title}</p>
      <p className="mt-1 text-xl font-semibold text-gray-900">{value}</p>
      {hint ? <p className="mt-1 text-xs text-gray-500">{hint}</p> : null}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section aria-label={title} className="rounded-lg bg-white p-4 shadow">
      <h2 className="text-sm font-semibold text-gray-900">{title}</h2>
      {children}
    </section>
  )
}

function DashboardContent({ dashboard }: { dashboard: Dashboard }) {
  const { currency, budget } = dashboard
  const health = healthMeta[budget.health]

  return (
    <div className="mt-6 space-y-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          title="Available balance"
          value={formatMoney(dashboard.availableBalance, currency)}
        />
        <SummaryCard
          title="Income this month"
          value={formatMoney(dashboard.mtdIncome, currency)}
        />
        <SummaryCard
          title="Spending this month"
          value={formatMoney(dashboard.mtdSpending, currency)}
        />
        <SummaryCard
          title="Remaining budget"
          value={budget.exists ? formatMoney(budget.totalRemaining, currency) : '—'}
          hint={
            budget.exists
              ? `${formatMoney(budget.totalBudgeted, currency)} budgeted`
              : 'Create this month’s budget to track it'
          }
        />
      </div>

      <p
        className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm font-medium ${health.className}`}
      >
        <span aria-hidden="true">{health.icon}</span>
        {health.label}
      </p>

      <div className="flex flex-wrap gap-2" aria-label="Quick actions" role="group">
        {quickActions.map((a) => (
          <Link
            key={a.label}
            to={a.to}
            className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500"
          >
            {a.label}
          </Link>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {dashboard.categoriesAtRisk.length > 0 ? (
          <Section title="Categories to watch">
            <ul className="mt-2 divide-y divide-gray-100">
              {dashboard.categoriesAtRisk.map((c) => (
                <li key={c.categoryId} className="flex items-center justify-between gap-2 py-2">
                  <div>
                    <p className="text-sm font-medium text-gray-900">{c.name}</p>
                    <p className="text-xs text-gray-600">
                      {formatMoney(c.remaining, currency)} remaining of{' '}
                      {formatMoney(c.budgeted + c.rollover, currency)}
                    </p>
                  </div>
                  <StatusBadge status={c.status} />
                </li>
              ))}
            </ul>
          </Section>
        ) : null}

        {dashboard.upcomingBills.length > 0 ? (
          <Section title="Upcoming bills">
            <ul className="mt-2 divide-y divide-gray-100">
              {dashboard.upcomingBills.map((b) => (
                <li key={b.ruleId} className="flex items-center justify-between gap-2 py-2">
                  <div>
                    <p className="text-sm font-medium text-gray-900">{b.name}</p>
                    <p className="text-xs text-gray-600">
                      Due {formatDate(b.dueDate)}
                      {b.daysUntilDue === 0
                        ? ' (today)'
                        : b.daysUntilDue === 1
                          ? ' (tomorrow)'
                          : ` (in ${b.daysUntilDue} days)`}
                    </p>
                  </div>
                  <p className="text-sm font-medium text-gray-900">
                    {formatMoney(b.amount, currency)}
                  </p>
                </li>
              ))}
            </ul>
            <Link
              to="/recurring"
              className="mt-2 inline-block text-sm font-medium text-indigo-600 hover:text-indigo-500"
            >
              All recurring bills
            </Link>
          </Section>
        ) : null}

        <Section title="Recent transactions">
          {dashboard.recentTransactions.length === 0 ? (
            <p className="mt-2 text-sm text-gray-600">No transactions yet.</p>
          ) : (
            <ul className="mt-2 divide-y divide-gray-100">
              {dashboard.recentTransactions.map((t) => (
                <li key={t.id} className="flex items-center justify-between gap-2 py-2">
                  <div>
                    <p className="text-sm font-medium text-gray-900">{t.payee || t.type}</p>
                    <p className="text-xs text-gray-600">{formatDate(t.date)}</p>
                  </div>
                  <p
                    className={`text-sm font-medium ${t.amount < 0 ? 'text-gray-900' : 'text-emerald-700'}`}
                  >
                    {formatMoney(t.amount, currency)}
                  </p>
                </li>
              ))}
            </ul>
          )}
          <Link
            to="/transactions"
            className="mt-2 inline-block text-sm font-medium text-indigo-600 hover:text-indigo-500"
          >
            All transactions
          </Link>
        </Section>

        {dashboard.goals.length > 0 ? (
          <Section title="Goal progress">
            <ul className="mt-2 space-y-3">
              {dashboard.goals.map((g) => {
                const pct =
                  g.targetAmount > 0
                    ? Math.max(0, Math.min(100, Math.round((g.currentBalance / g.targetAmount) * 100)))
                    : 0
                return (
                  <li key={g.id}>
                    <div className="flex items-center justify-between gap-2">
                      <p className="text-sm font-medium text-gray-900">{g.name}</p>
                      <p className="text-xs text-gray-600">
                        {formatMoney(g.currentBalance, currency)} of{' '}
                        {formatMoney(g.targetAmount, currency)}
                        {g.behindSchedule ? ' · behind schedule' : ''}
                      </p>
                    </div>
                    <div
                      role="progressbar"
                      aria-label={`${g.name} progress`}
                      aria-valuenow={pct}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuetext={`${pct}% of target saved`}
                      className="mt-1 h-1.5 overflow-hidden rounded-full bg-gray-200"
                    >
                      <div className="h-full rounded-full bg-indigo-600" style={{ width: `${pct}%` }} />
                    </div>
                  </li>
                )
              })}
            </ul>
            <Link
              to="/goals"
              className="mt-3 inline-block text-sm font-medium text-indigo-600 hover:text-indigo-500"
            >
              All goals
            </Link>
          </Section>
        ) : null}

        {dashboard.foreignBalances.length > 0 ? (
          <Section title="Other currencies">
            <p className="mt-1 text-xs text-gray-600">
              Informational balances — not included in {currency} totals.
            </p>
            <ul className="mt-2 divide-y divide-gray-100">
              {dashboard.foreignBalances.map((a) => (
                <li key={a.accountId} className="flex items-center justify-between gap-2 py-2">
                  <p className="text-sm font-medium text-gray-900">{a.name}</p>
                  <p className="text-sm text-gray-700">{formatMoney(a.balance, a.currency)}</p>
                </li>
              ))}
            </ul>
          </Section>
        ) : null}
      </div>
    </div>
  )
}

export function DashboardPage() {
  const { data: dashboard, isLoading, error } = useDashboard()

  const isFirstUse =
    dashboard !== undefined &&
    dashboard.availableBalance === 0 &&
    !dashboard.budget.exists &&
    dashboard.recentTransactions.length === 0 &&
    dashboard.upcomingBills.length === 0 &&
    dashboard.goals.length === 0

  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
      {isLoading ? <p className="mt-6 text-sm text-gray-600">Loading…</p> : null}
      {error ? (
        <p role="alert" className="mt-6 text-sm text-red-600">
          {error.message}
        </p>
      ) : null}
      {isFirstUse ? (
        <EmptyState
          title="Welcome to BudgetFlow"
          description="Your dashboard will show balances, monthly spending, budget health, upcoming bills, and goals once you add an account and a few transactions."
          actionLabel="Add your first transaction"
          actionTo="/transactions?quick-add=1"
        />
      ) : dashboard ? (
        <DashboardContent dashboard={dashboard} />
      ) : null}
    </>
  )
}
