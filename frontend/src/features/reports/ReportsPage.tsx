import { useState } from 'react'
import {
  budgetCsvUrl,
  categoriesCsvUrl,
  goalsCsvUrl,
  transactionsCsvUrl,
} from '../../api/reports'
import { EmptyState } from '../../components/EmptyState'
import { Field, SelectField } from '../../components/forms/Field'
import { formatMoney } from '../../lib/money'
import { useCurrentUser } from '../auth/useAuth'
import {
  useCashFlow,
  useIncomeVsExpenses,
  useMonthlyTrend,
  useNetWorth,
  useSpendingByCategory,
  useTopPayees,
} from './useReports'

const monthNames = [
  'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
]

function monthLabel(year: number, month: number): string {
  return `${monthNames[month - 1]} ${year}`
}

// First day of the current month / today, as YYYY-MM-DD range defaults —
// mirrors the backend's default when from/to are omitted.
function defaultRange(): { from: string; to: string } {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return {
    from: `${now.getFullYear()}-${pad(now.getMonth() + 1)}-01`,
    to: `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`,
  }
}

function downloadLinkClass() {
  return 'rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500'
}

// Horizontal bar rendered next to table rows; decorative only — the table
// text carries the data, so the bar is hidden from assistive tech.
function Bar({ value, max, className }: { value: number; max: number; className: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (Math.abs(value) / max) * 100)) : 0
  return (
    <div aria-hidden="true" className="h-2 w-full min-w-24 overflow-hidden rounded-full bg-gray-100">
      <div className={`h-full rounded-full ${className}`} style={{ width: `${pct}%` }} />
    </div>
  )
}

function ReportSection({
  title,
  description,
  action,
  children,
}: {
  title: string
  description?: string
  action?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section aria-label={title} className="rounded-lg bg-white p-4 shadow">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-gray-900">{title}</h2>
        {action}
      </div>
      {description ? <p className="mt-1 text-sm text-gray-600">{description}</p> : null}
      {children}
    </section>
  )
}

const thClass = 'py-2 pr-4 text-left text-xs font-semibold text-gray-600'
const tdClass = 'py-2 pr-4 text-sm text-gray-900'
const tdNumClass = 'py-2 pr-4 text-right text-sm tabular-nums text-gray-900'
const thNumClass = 'py-2 pr-4 text-right text-xs font-semibold text-gray-600'

export function ReportsPage() {
  const { data: user } = useCurrentUser()
  const currency = user?.defaultCurrency ?? 'USD'
  const [range, setRange] = useState(defaultRange)
  const [months, setMonths] = useState(6)

  const spending = useSpendingByCategory(range.from, range.to)
  const trend = useMonthlyTrend(months)
  const incomeVsExpenses = useIncomeVsExpenses(months)
  const cashFlow = useCashFlow(months)
  const netWorth = useNetWorth()
  const topPayees = useTopPayees(range.from, range.to)

  const now = new Date()
  const isLoading = spending.isLoading || trend.isLoading || netWorth.isLoading
  const isEmpty =
    !isLoading &&
    (spending.data?.categories.length ?? 0) === 0 &&
    (trend.data ?? []).every((m) => m.spending === 0) &&
    (netWorth.data?.accounts.length ?? 0) === 0

  const maxSpending = Math.max(0, ...(spending.data?.categories.map((c) => c.spending) ?? []))
  const maxTrend = Math.max(0, ...(trend.data?.map((m) => m.spending) ?? []))

  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Reports</h1>

      {isEmpty ? (
        <EmptyState
          title="Nothing to report yet"
          description="Spending by category, monthly trends, cash flow, and net worth will appear here once you have transactions."
          actionLabel="Go to transactions"
          actionTo="/transactions"
        />
      ) : (
        <div className="mt-6 space-y-6">
          <div className="flex flex-wrap items-end gap-3">
            <Field
              label="From"
              type="date"
              value={range.from}
              onChange={(e) => setRange((r) => ({ ...r, from: e.target.value }))}
            />
            <Field
              label="To"
              type="date"
              value={range.to}
              onChange={(e) => setRange((r) => ({ ...r, to: e.target.value }))}
            />
            <SelectField
              label="Trend window"
              value={String(months)}
              onChange={(e) => setMonths(Number(e.target.value))}
            >
              <option value="3">Last 3 months</option>
              <option value="6">Last 6 months</option>
              <option value="12">Last 12 months</option>
            </SelectField>
          </div>

          <ReportSection
            title="Spending by category"
            description={
              spending.data
                ? `Total spending ${formatMoney(spending.data.totalSpending, currency)} between ${spending.data.from} and ${spending.data.to}.`
                : undefined
            }
            action={
              <a href={transactionsCsvUrl(range.from, range.to)} className={downloadLinkClass()}>
                Download transactions CSV
              </a>
            }
          >
            {spending.data && spending.data.categories.length > 0 ? (
              <table className="mt-3 w-full">
                <thead>
                  <tr>
                    <th scope="col" className={thClass}>Category</th>
                    <th scope="col" className={thClass}>Group</th>
                    <th scope="col" className={thNumClass}>Spending</th>
                    <th scope="col" className="w-1/3 py-2">
                      <span className="sr-only">Share of total</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {spending.data.categories.map((c) => (
                    <tr key={c.categoryId}>
                      <td className={tdClass}>{c.name}</td>
                      <td className={`${tdClass} text-gray-600`}>{c.group}</td>
                      <td className={tdNumClass}>{formatMoney(c.spending, currency)}</td>
                      <td className="py-2">
                        <Bar value={c.spending} max={maxSpending} className="bg-indigo-500" />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="mt-3 text-sm text-gray-600">No spending in this period.</p>
            )}
          </ReportSection>

          <ReportSection title="Monthly spending trend">
            {trend.data ? (
              <table className="mt-3 w-full">
                <thead>
                  <tr>
                    <th scope="col" className={thClass}>Month</th>
                    <th scope="col" className={thNumClass}>Spending</th>
                    <th scope="col" className="w-1/2 py-2">
                      <span className="sr-only">Relative spending</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {trend.data.map((m) => (
                    <tr key={`${m.year}-${m.month}`}>
                      <td className={tdClass}>{monthLabel(m.year, m.month)}</td>
                      <td className={tdNumClass}>{formatMoney(m.spending, currency)}</td>
                      <td className="py-2">
                        <Bar value={m.spending} max={maxTrend} className="bg-indigo-500" />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
          </ReportSection>

          <ReportSection
            title="Income vs expenses"
            action={
              <a href={budgetCsvUrl(now.getFullYear(), now.getMonth() + 1)} className={downloadLinkClass()}>
                Download budget CSV
              </a>
            }
          >
            {incomeVsExpenses.data ? (
              <table className="mt-3 w-full">
                <thead>
                  <tr>
                    <th scope="col" className={thClass}>Month</th>
                    <th scope="col" className={thNumClass}>Income</th>
                    <th scope="col" className={thNumClass}>Expenses</th>
                    <th scope="col" className={thNumClass}>Net</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {incomeVsExpenses.data.map((m) => (
                    <tr key={`${m.year}-${m.month}`}>
                      <td className={tdClass}>{monthLabel(m.year, m.month)}</td>
                      <td className={tdNumClass}>{formatMoney(m.income, currency)}</td>
                      <td className={tdNumClass}>{formatMoney(m.expenses, currency)}</td>
                      <td className={`${tdNumClass} ${m.net < 0 ? 'text-red-700' : 'text-emerald-700'}`}>
                        {formatMoney(m.net, currency)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
          </ReportSection>

          <ReportSection
            title="Cash flow"
            description="All money in and out of your accounts, including transfers between them."
          >
            {cashFlow.data ? (
              <table className="mt-3 w-full">
                <thead>
                  <tr>
                    <th scope="col" className={thClass}>Month</th>
                    <th scope="col" className={thNumClass}>Inflow</th>
                    <th scope="col" className={thNumClass}>Outflow</th>
                    <th scope="col" className={thNumClass}>Net</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {cashFlow.data.map((m) => (
                    <tr key={`${m.year}-${m.month}`}>
                      <td className={tdClass}>{monthLabel(m.year, m.month)}</td>
                      <td className={tdNumClass}>{formatMoney(m.inflow, currency)}</td>
                      <td className={tdNumClass}>{formatMoney(m.outflow, currency)}</td>
                      <td className={`${tdNumClass} ${m.net < 0 ? 'text-red-700' : 'text-emerald-700'}`}>
                        {formatMoney(m.net, currency)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
          </ReportSection>

          <ReportSection
            title="Net worth"
            description={
              netWorth.data
                ? `Total across included accounts: ${formatMoney(netWorth.data.total, netWorth.data.currency)}.`
                : undefined
            }
          >
            {netWorth.data ? (
              <>
                <table className="mt-3 w-full">
                  <thead>
                    <tr>
                      <th scope="col" className={thClass}>Account</th>
                      <th scope="col" className={thClass}>Type</th>
                      <th scope="col" className={thNumClass}>Balance</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100">
                    {netWorth.data.accounts.map((a) => (
                      <tr key={a.accountId}>
                        <td className={tdClass}>{a.name}</td>
                        <td className={`${tdClass} text-gray-600`}>{a.type.replace('_', ' ')}</td>
                        <td className={tdNumClass}>{formatMoney(a.balance, a.currency)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {netWorth.data.foreignBalances.length > 0 ? (
                  <div className="mt-3 border-t border-gray-100 pt-3">
                    <p className="text-xs text-gray-600">
                      Other currencies (informational, not converted):
                    </p>
                    <ul className="mt-1 space-y-1">
                      {netWorth.data.foreignBalances.map((a) => (
                        <li key={a.accountId} className="flex justify-between text-sm text-gray-700">
                          <span>{a.name}</span>
                          <span className="tabular-nums">{formatMoney(a.balance, a.currency)}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </>
            ) : null}
          </ReportSection>

          <ReportSection title="Top payees">
            {topPayees.data && topPayees.data.payees.length > 0 ? (
              <table className="mt-3 w-full">
                <thead>
                  <tr>
                    <th scope="col" className={thClass}>Payee</th>
                    <th scope="col" className={thNumClass}>Transactions</th>
                    <th scope="col" className={thNumClass}>Spending</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {topPayees.data.payees.map((p) => (
                    <tr key={p.payee}>
                      <td className={tdClass}>{p.payee}</td>
                      <td className={tdNumClass}>{p.transactionCount}</td>
                      <td className={tdNumClass}>{formatMoney(p.spending, currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="mt-3 text-sm text-gray-600">No payees in this period.</p>
            )}
          </ReportSection>

          <div className="flex flex-wrap gap-2">
            <a href={categoriesCsvUrl} className={downloadLinkClass()}>
              Download categories CSV
            </a>
            <a href={goalsCsvUrl} className={downloadLinkClass()}>
              Download goals CSV
            </a>
          </div>
        </div>
      )}
    </>
  )
}
