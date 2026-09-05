import { screen, within } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Dashboard } from '../../api/reports'
import { runAxe } from '../../test/axe'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { DashboardPage } from './DashboardPage'

const fixture: Dashboard = {
  currency: 'UAH',
  availableBalance: 1234500,
  foreignBalances: [
    {
      accountId: 'a0000000-0000-4000-8000-000000000002',
      name: 'EUR savings',
      type: 'savings',
      currency: 'EUR',
      balance: 50000,
    },
  ],
  mtdIncome: 500000,
  mtdSpending: 210050,
  budget: {
    exists: true,
    year: 2026,
    month: 9,
    plannedIncome: 500000,
    unallocated: 20000,
    totalBudgeted: 480000,
    totalSpending: 210050,
    totalRemaining: 269950,
    health: 'approaching_limit',
  },
  categoriesAtRisk: [
    {
      categoryId: 'c0000000-0000-4000-8000-000000000001',
      name: 'Groceries',
      budgeted: 80000,
      rollover: 0,
      spending: 70000,
      reserved: 0,
      remaining: 10000,
      status: 'approaching_limit',
    },
  ],
  upcomingBills: [
    {
      ruleId: 'r0000000-0000-4000-8000-000000000001',
      name: 'Rent',
      amount: 150000,
      accountId: 'a0000000-0000-4000-8000-000000000001',
      categoryId: null,
      dueDate: '2026-09-10',
      daysUntilDue: 5,
    },
  ],
  recentTransactions: [
    {
      id: 't0000000-0000-4000-8000-000000000001',
      accountId: 'a0000000-0000-4000-8000-000000000001',
      categoryId: null,
      type: 'expense',
      status: 'cleared',
      amount: -4250,
      date: '2026-09-04',
      payee: 'Silpo',
      notes: '',
    },
  ],
  goals: [
    {
      id: 'g0000000-0000-4000-8000-000000000001',
      name: 'Emergency fund',
      type: 'savings',
      targetAmount: 100000,
      targetDate: '2027-03-01',
      currentBalance: 25000,
      amountRemaining: 75000,
      requiredMonthlyContribution: 12500,
      behindSchedule: false,
    },
  ],
}

function renderDashboard(dashboard: Dashboard) {
  mockFetch((url) => {
    if (url.startsWith('/api/dashboard')) {
      return jsonResponse(200, dashboard)
    }
    if (url.startsWith('/api/auth/me')) {
      return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
    }
    return jsonResponse(404, { error: `unhandled ${url}` })
  })
  return renderWithProviders(
    <Routes>
      <Route path="/" element={<DashboardPage />} />
    </Routes>,
  )
}

describe('DashboardPage', () => {
  it('renders summary cards, health indicator, and all sections from the API response', async () => {
    renderDashboard(fixture)

    // Summary cards (amounts arrive as minor units).
    expect(await screen.findByText(/UAH\s?12,345\.00/)).toBeInTheDocument()
    expect(screen.getByText('Income this month')).toBeInTheDocument()
    expect(screen.getByText(/UAH\s?5,000\.00/)).toBeInTheDocument()
    expect(screen.getByText('Spending this month')).toBeInTheDocument()
    expect(screen.getByText(/UAH\s?2,699\.50/)).toBeInTheDocument()

    // Health is conveyed with text, not color alone.
    expect(screen.getByText('Some categories approaching their limit')).toBeInTheDocument()

    // At-risk category with its status badge.
    const atRisk = screen.getByRole('region', { name: 'Categories to watch' })
    expect(within(atRisk).getByText('Groceries')).toBeInTheDocument()
    expect(within(atRisk).getByText('Approaching limit')).toBeInTheDocument()

    // Upcoming bill with due date and amount.
    const bills = screen.getByRole('region', { name: 'Upcoming bills' })
    expect(within(bills).getByText('Rent')).toBeInTheDocument()
    expect(within(bills).getByText(/Due Sep 10, 2026/)).toBeInTheDocument()
    expect(within(bills).getByText(/UAH\s?1,500\.00/)).toBeInTheDocument()

    // Recent transaction and goal progress.
    const recent = screen.getByRole('region', { name: 'Recent transactions' })
    expect(within(recent).getByText('Silpo')).toBeInTheDocument()
    const goals = screen.getByRole('region', { name: 'Goal progress' })
    const bar = within(goals).getByRole('progressbar', { name: 'Emergency fund progress' })
    expect(bar).toHaveAttribute('aria-valuenow', '25')

    // Foreign balances are informational, in their own currency.
    const foreign = screen.getByRole('region', { name: 'Other currencies' })
    expect(within(foreign).getByText(/€\s?500\.00|EUR\s?500\.00/)).toBeInTheDocument()
  })

  it('offers quick actions linking to the right screens', async () => {
    renderDashboard(fixture)

    expect(await screen.findByRole('link', { name: 'Add transaction' })).toHaveAttribute(
      'href',
      '/transactions?quick-add=1',
    )
    expect(screen.getByRole('link', { name: 'Create category' })).toHaveAttribute(
      'href',
      '/categories',
    )
    expect(screen.getByRole('link', { name: 'Allocate funds' })).toHaveAttribute('href', '/budget')
  })

  it('shows the first-use empty state when there is no activity at all', async () => {
    renderDashboard({
      ...fixture,
      availableBalance: 0,
      foreignBalances: [],
      mtdIncome: 0,
      mtdSpending: 0,
      budget: { ...fixture.budget, exists: false, health: 'no_budget' },
      categoriesAtRisk: [],
      upcomingBills: [],
      recentTransactions: [],
      goals: [],
    })

    expect(await screen.findByText('Welcome to BudgetFlow')).toBeInTheDocument()
    expect(screen.queryByText('Available balance')).not.toBeInTheDocument()
  })

  it('has no axe violations on the fully populated dashboard', async () => {
    const { container } = renderDashboard(fixture)
    await screen.findByText('Available balance')
    expect(await runAxe(container)).toHaveNoViolations()
  })
})
