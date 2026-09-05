import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Goal } from '../../api/goals'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { GoalsPage } from './GoalsPage'

const emergencyFund: Goal = {
  id: 'g0000000-0000-4000-8000-000000000001',
  name: 'Emergency fund',
  type: 'savings',
  targetAmount: 100000,
  targetDate: '2027-03-01',
  categoryId: null,
  accountId: null,
  archived: false,
  currentBalance: 25000,
  amountRemaining: 75000,
  monthsRemaining: 6,
  requiredMonthlyContribution: 12500,
  behindSchedule: true,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

function baseHandler(goals: Goal[]) {
  return (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/goals') && (!init?.method || init.method === 'GET')) {
      return jsonResponse(200, { goals })
    }
    if (url.startsWith('/api/accounts')) {
      return jsonResponse(200, { accounts: [] })
    }
    if (url.startsWith('/api/categories')) {
      return jsonResponse(200, { groups: [] })
    }
    if (url.startsWith('/api/auth/me')) {
      return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
    }
    return jsonResponse(404, { error: `unhandled ${url}` })
  }
}

function renderGoals() {
  return renderWithProviders(
    <Routes>
      <Route path="/goals" element={<GoalsPage />} />
    </Routes>,
    { route: '/goals' },
  )
}

describe('GoalsPage', () => {
  it('renders goal progress with an accessible bar, amounts, and schedule status', async () => {
    mockFetch(baseHandler([emergencyFund]))
    renderGoals()

    const card = await screen.findByRole('region', { name: 'Emergency fund' })
    const bar = within(card).getByRole('progressbar', { name: 'Emergency fund progress' })
    expect(bar).toHaveAttribute('aria-valuenow', '25')
    // Amounts use the user's default currency (testUser is UAH).
    expect(within(card).getByText(/UAH\s?250\.00 of UAH\s?1,000\.00/)).toBeInTheDocument()
    expect(within(card).getByText('Behind schedule')).toBeInTheDocument()
    expect(
      within(card).getByText(/Contribute UAH\s?125\.00\/month to stay on\s+schedule\./),
    ).toBeInTheDocument()
  })

  it('shows an on-track badge when the goal keeps pace', async () => {
    mockFetch(baseHandler([{ ...emergencyFund, behindSchedule: false }]))
    renderGoals()

    const card = await screen.findByRole('region', { name: 'Emergency fund' })
    expect(within(card).getByText('On track')).toBeInTheDocument()
    expect(within(card).queryByText('Behind schedule')).not.toBeInTheDocument()
  })

  it('records a contribution in minor units', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/goals/${emergencyFund.id}/contributions` && init?.method === 'POST') {
        return jsonResponse(201, {
          contribution: { id: 'c1', goalId: emergencyFund.id, amount: 5000 },
          goal: { ...emergencyFund, currentBalance: 30000 },
        })
      }
      return baseHandler([emergencyFund])(url, init)
    })
    renderGoals()

    await userEvent.click(await screen.findByRole('button', { name: 'Add contribution' }))
    await userEvent.type(screen.getByLabelText('Contribution amount'), '50.00')
    await userEvent.click(screen.getByRole('button', { name: 'Record contribution' }))

    const call = fetchMock.mock.calls.find(
      ([url, init]) =>
        url === `/api/goals/${emergencyFund.id}/contributions` && init?.method === 'POST',
    )
    expect(call).toBeDefined()
    const body = JSON.parse(String(call![1]!.body)) as { amount: number; date: string }
    expect(body.amount).toBe(5000)
    expect(body.date).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  })

  it('creates a goal from the form', async () => {
    const goals: Goal[] = []
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/goals' && init?.method === 'POST') {
        const body = JSON.parse(String(init.body)) as Goal
        goals.push({ ...emergencyFund, ...body, id: 'g0000000-0000-4000-8000-000000000002' })
        return jsonResponse(201, { goal: goals[0] })
      }
      return baseHandler(goals)(url, init)
    })
    renderGoals()

    await userEvent.click(await screen.findByRole('button', { name: 'Add your first goal' }))
    await userEvent.type(screen.getByLabelText('Goal name'), 'New laptop')
    await userEvent.selectOptions(screen.getByLabelText('Goal type'), 'purchase')
    await userEvent.type(screen.getByLabelText('Target amount'), '2000.00')
    await userEvent.click(screen.getByRole('button', { name: 'Create goal' }))

    expect(await screen.findByRole('region', { name: 'New laptop' })).toBeInTheDocument()
    const call = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/goals' && init?.method === 'POST',
    )
    expect(JSON.parse(String(call![1]!.body))).toEqual({
      name: 'New laptop',
      type: 'purchase',
      targetAmount: 200000,
      targetDate: null,
      categoryId: null,
      accountId: null,
    })
  })
})
