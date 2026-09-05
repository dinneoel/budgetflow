import { screen, within } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { ReportsPage } from './ReportsPage'

function baseHandler() {
  return (url: string) => {
    if (url.startsWith('/api/reports/spending-by-category')) {
      return jsonResponse(200, {
        from: '2026-09-01',
        to: '2026-09-30',
        totalSpending: 130000,
        categories: [
          { categoryId: 'c1', name: 'Groceries', group: 'Essentials', spending: 80000 },
          { categoryId: 'c2', name: 'Dining out', group: 'Lifestyle', spending: 50000 },
        ],
      })
    }
    if (url.startsWith('/api/reports/monthly-trend')) {
      return jsonResponse(200, {
        months: [
          { year: 2026, month: 8, spending: 200000 },
          { year: 2026, month: 9, spending: 130000 },
        ],
      })
    }
    if (url.startsWith('/api/reports/income-vs-expenses')) {
      return jsonResponse(200, {
        months: [{ year: 2026, month: 9, income: 500000, expenses: 130000, net: 370000 }],
      })
    }
    if (url.startsWith('/api/reports/cash-flow')) {
      return jsonResponse(200, {
        months: [{ year: 2026, month: 9, inflow: 500000, outflow: 150000, net: 350000 }],
      })
    }
    if (url.startsWith('/api/reports/net-worth')) {
      return jsonResponse(200, {
        currency: 'UAH',
        total: 1234500,
        accounts: [
          { accountId: 'a1', name: 'Main checking', type: 'checking', currency: 'UAH', balance: 1234500 },
        ],
        foreignBalances: [
          { accountId: 'a2', name: 'EUR savings', type: 'savings', currency: 'EUR', balance: 50000 },
        ],
        foreignTotals: { EUR: 50000 },
      })
    }
    if (url.startsWith('/api/reports/top-payees')) {
      return jsonResponse(200, {
        from: '2026-09-01',
        to: '2026-09-30',
        payees: [{ payee: 'Silpo', transactionCount: 4, spending: 42000 }],
      })
    }
    if (url.startsWith('/api/auth/me')) {
      return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
    }
    return jsonResponse(404, { error: `unhandled ${url}` })
  }
}

function renderReports() {
  mockFetch(baseHandler())
  return renderWithProviders(
    <Routes>
      <Route path="/reports" element={<ReportsPage />} />
    </Routes>,
    { route: '/reports' },
  )
}

describe('ReportsPage', () => {
  it('renders every report section as an accessible table from fixture responses', async () => {
    renderReports()

    const spending = await screen.findByRole('region', { name: 'Spending by category' })
    expect(await within(spending).findByText('Groceries')).toBeInTheDocument()
    expect(within(spending).getByText(/UAH\s?800\.00/)).toBeInTheDocument()
    expect(within(spending).getByText(/Total spending UAH\s?1,300\.00/)).toBeInTheDocument()

    const trend = screen.getByRole('region', { name: 'Monthly spending trend' })
    expect(await within(trend).findByText('Aug 2026')).toBeInTheDocument()
    expect(within(trend).getByText(/UAH\s?2,000\.00/)).toBeInTheDocument()

    const ive = screen.getByRole('region', { name: 'Income vs expenses' })
    expect(await within(ive).findByText(/UAH\s?5,000\.00/)).toBeInTheDocument()
    expect(within(ive).getByText(/UAH\s?3,700\.00/)).toBeInTheDocument()

    const cashFlow = screen.getByRole('region', { name: 'Cash flow' })
    expect(await within(cashFlow).findByText(/UAH\s?3,500\.00/)).toBeInTheDocument()

    const netWorth = screen.getByRole('region', { name: 'Net worth' })
    expect(await within(netWorth).findByText('Main checking')).toBeInTheDocument()
    // Foreign account shown in its own currency, never converted.
    expect(within(netWorth).getByText(/€\s?500\.00|EUR\s?500\.00/)).toBeInTheDocument()

    const payees = screen.getByRole('region', { name: 'Top payees' })
    expect(await within(payees).findByText('Silpo')).toBeInTheDocument()
    expect(within(payees).getByText('4')).toBeInTheDocument()
  })

  it('exposes CSV download links pointing at the export endpoints', async () => {
    renderReports()

    const txLink = await screen.findByRole('link', { name: 'Download transactions CSV' })
    const href = txLink.getAttribute('href') ?? ''
    expect(href.startsWith('/api/export/transactions.csv?')).toBe(true)
    expect(href).toMatch(/from=\d{4}-\d{2}-\d{2}/)
    expect(href).toMatch(/to=\d{4}-\d{2}-\d{2}/)

    expect(
      screen.getByRole('link', { name: 'Download budget CSV' }).getAttribute('href'),
    ).toMatch(/^\/api\/export\/budget\.csv\?year=\d{4}&month=\d{1,2}$/)
    expect(screen.getByRole('link', { name: 'Download categories CSV' })).toHaveAttribute(
      'href',
      '/api/export/categories.csv',
    )
    expect(screen.getByRole('link', { name: 'Download goals CSV' })).toHaveAttribute(
      'href',
      '/api/export/goals.csv',
    )
  })

  it('shows the empty state when there is no report data', async () => {
    mockFetch((url: string) => {
      if (url.startsWith('/api/reports/spending-by-category')) {
        return jsonResponse(200, { from: '2026-09-01', to: '2026-09-30', totalSpending: 0, categories: [] })
      }
      if (url.startsWith('/api/reports/monthly-trend')) {
        return jsonResponse(200, { months: [{ year: 2026, month: 9, spending: 0 }] })
      }
      if (url.startsWith('/api/reports/income-vs-expenses') || url.startsWith('/api/reports/cash-flow')) {
        return jsonResponse(200, { months: [] })
      }
      if (url.startsWith('/api/reports/net-worth')) {
        return jsonResponse(200, { currency: 'UAH', total: 0, accounts: [], foreignBalances: [], foreignTotals: {} })
      }
      if (url.startsWith('/api/reports/top-payees')) {
        return jsonResponse(200, { from: '2026-09-01', to: '2026-09-30', payees: [] })
      }
      if (url.startsWith('/api/auth/me')) {
        return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
      }
      return jsonResponse(404, { error: `unhandled ${url}` })
    })
    renderWithProviders(
      <Routes>
        <Route path="/reports" element={<ReportsPage />} />
      </Routes>,
      { route: '/reports' },
    )

    expect(await screen.findByText('Nothing to report yet')).toBeInTheDocument()
  })
})
