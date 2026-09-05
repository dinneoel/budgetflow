import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from './App'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from './test/utils'

describe('App', () => {
  it('sends unauthenticated visitors to the sign-in page', async () => {
    mockFetch(() => jsonResponse(401, { error: 'unauthenticated' }))
    renderWithProviders(<App />, { route: '/' })

    expect(
      await screen.findByRole('heading', { name: 'Sign in to your account' }),
    ).toBeInTheDocument()
  })

  it('shows the dashboard empty state for a signed-in user', async () => {
    mockFetch((url) => {
      if (url.startsWith('/api/dashboard')) {
        return jsonResponse(200, {
          currency: testUser.defaultCurrency,
          availableBalance: 0,
          foreignBalances: [],
          mtdIncome: 0,
          mtdSpending: 0,
          budget: {
            exists: false,
            year: 2026,
            month: 9,
            plannedIncome: 0,
            unallocated: 0,
            totalBudgeted: 0,
            totalSpending: 0,
            totalRemaining: 0,
            health: 'no_budget',
          },
          categoriesAtRisk: [],
          upcomingBills: [],
          recentTransactions: [],
          goals: [],
        })
      }
      if (url.startsWith('/api/notifications')) {
        return jsonResponse(200, { count: 0 })
      }
      return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
    })
    renderWithProviders(<App />, { route: '/' })

    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(await screen.findByText('Welcome to BudgetFlow')).toBeInTheDocument()
  })
})
