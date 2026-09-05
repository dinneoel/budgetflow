import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Account } from '../../api/accounts'
import type { CategoryGroup } from '../../api/categories'
import type { RecurringRule } from '../../api/recurring'
import type { Transaction } from '../../api/transactions'
import { jsonResponse, mockFetch, renderWithProviders } from '../../test/utils'
import { RecurringPage } from './RecurringPage'

const checking: Account = {
  id: 'a0000000-0000-4000-8000-000000000001',
  name: 'Checking',
  institution: '',
  type: 'checking',
  currency: 'USD',
  openingBalance: 0,
  balance: 100000,
  includeInNetWorth: true,
  archivedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

const groups: CategoryGroup[] = [
  {
    id: 'g0000000-0000-4000-8000-000000000001',
    name: 'Housing',
    sortOrder: 0,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
    categories: [
      {
        id: 'c0000000-0000-4000-8000-000000000001',
        groupId: 'g0000000-0000-4000-8000-000000000001',
        name: 'Rent',
        icon: '',
        color: '',
        budgetType: 'fixed',
        rolloverRule: 'none',
        sortOrder: 0,
        archivedAt: null,
        createdAt: '2026-09-01T00:00:00Z',
        updatedAt: '2026-09-01T00:00:00Z',
      },
    ],
  },
]

const rentRule: RecurringRule = {
  id: 'r0000000-0000-4000-8000-000000000001',
  name: 'Rent',
  accountId: checking.id,
  categoryId: groups[0].categories[0].id,
  amount: 120000,
  frequency: 'monthly',
  customIntervalDays: null,
  nextDueDate: '2026-10-01',
  reminderLeadDays: 3,
  archived: false,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

const pastExpense: Transaction = {
  id: 't0000000-0000-4000-8000-000000000001',
  accountId: checking.id,
  categoryId: groups[0].categories[0].id,
  type: 'expense',
  status: 'cleared',
  amount: -120000,
  date: '2026-09-28',
  payee: 'Landlord',
  notes: '',
  reviewed: false,
  transferPairId: null,
  deletedAt: null,
  createdAt: '2026-09-28T00:00:00Z',
  updatedAt: '2026-09-28T00:00:00Z',
  splits: [],
  tags: [],
}

function baseHandler(rules: RecurringRule[]) {
  return (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/recurring') && (!init?.method || init.method === 'GET')) {
      return jsonResponse(200, { rules })
    }
    if (url.startsWith('/api/accounts')) {
      return jsonResponse(200, { accounts: [checking] })
    }
    if (url.startsWith('/api/categories')) {
      return jsonResponse(200, { groups })
    }
    if (url.startsWith('/api/transactions')) {
      return jsonResponse(200, { transactions: [pastExpense], total: 1, limit: 10, offset: 0 })
    }
    return jsonResponse(404, { error: `unhandled ${url}` })
  }
}

function renderRecurring() {
  return renderWithProviders(
    <Routes>
      <Route path="/recurring" element={<RecurringPage />} />
    </Routes>,
    { route: '/recurring' },
  )
}

describe('RecurringPage', () => {
  it('lists rules with amount, schedule, and next due date', async () => {
    mockFetch(baseHandler([rentRule]))
    renderRecurring()

    expect(await screen.findByText('Rent')).toBeInTheDocument()
    expect(screen.getByText('$1,200.00')).toBeInTheDocument()
    expect(screen.getByText('Due Oct 1, 2026')).toBeInTheDocument()
    expect(screen.getByText(/Monthly/)).toBeInTheDocument()
  })

  it('creates a rule and shows its next due date', async () => {
    const rules: RecurringRule[] = []
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/recurring' && init?.method === 'POST') {
        const body = JSON.parse(String(init.body)) as RecurringRule
        rules.push({ ...rentRule, ...body, id: 'r0000000-0000-4000-8000-000000000099' })
        return jsonResponse(201, { rule: rules[0] })
      }
      return baseHandler(rules)(url, init)
    })
    renderRecurring()

    await userEvent.click(await screen.findByRole('button', { name: 'Add your first rule' }))
    await userEvent.type(screen.getByLabelText('Name'), 'Rent')
    await userEvent.selectOptions(screen.getByLabelText('Account'), checking.id)
    await userEvent.selectOptions(screen.getByLabelText('Category'), groups[0].categories[0].id)
    await userEvent.type(screen.getByLabelText('Amount'), '1200.00')
    fireEvent.change(screen.getByLabelText('Next due date'), { target: { value: '2026-10-01' } })
    await userEvent.click(screen.getByRole('button', { name: 'Create rule' }))

    expect(await screen.findByText('Due Oct 1, 2026')).toBeInTheDocument()
    const createCall = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/recurring' && init?.method === 'POST',
    )
    expect(createCall).toBeDefined()
    expect(JSON.parse(String(createCall![1]!.body))).toEqual({
      name: 'Rent',
      accountId: checking.id,
      categoryId: groups[0].categories[0].id,
      amount: 120000,
      frequency: 'monthly',
      customIntervalDays: null,
      nextDueDate: '2026-10-01',
      reminderLeadDays: 3,
    })
  })

  it('marks a rule paid', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/recurring/${rentRule.id}/pay` && init?.method === 'POST') {
        return jsonResponse(201, {
          rule: { ...rentRule, nextDueDate: '2026-11-01' },
          transaction: { id: 'tx', amount: -rentRule.amount },
        })
      }
      return baseHandler([rentRule])(url, init)
    })
    renderRecurring()

    await userEvent.click(await screen.findByRole('button', { name: 'Mark paid' }))

    const payCall = fetchMock.mock.calls.find(([url]) => url === `/api/recurring/${rentRule.id}/pay`)
    expect(payCall).toBeDefined()
  })

  it('matches an existing transaction to a rule', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/recurring/${rentRule.id}/match` && init?.method === 'POST') {
        return jsonResponse(200, {
          rule: { ...rentRule, nextDueDate: '2026-11-01' },
          transaction: { id: pastExpense.id },
        })
      }
      return baseHandler([rentRule])(url, init)
    })
    renderRecurring()

    await userEvent.click(await screen.findByRole('button', { name: 'Match transaction' }))
    expect(await screen.findByText(/Landlord/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Match' }))

    const matchCall = fetchMock.mock.calls.find(
      ([url]) => url === `/api/recurring/${rentRule.id}/match`,
    )
    expect(matchCall).toBeDefined()
    expect(JSON.parse(String(matchCall![1]!.body))).toEqual({ transactionId: pastExpense.id })
  })
})
