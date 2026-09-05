import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { AllocationDetail, BudgetPeriodDetail } from '../../api/budgets'
import { jsonResponse, mockFetch, mockViewport, renderWithProviders } from '../../test/utils'
import { BudgetPage } from './BudgetPage'

function category(id: string, name: string, sortOrder: number) {
  return {
    id,
    groupId: 'g1',
    name,
    icon: '🛒',
    color: '#dbeafe',
    budgetType: 'variable',
    rolloverRule: 'none',
    sortOrder,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
  }
}

const groups = [
  {
    id: 'g1',
    name: 'Essentials',
    sortOrder: 0,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
    categories: [
      category('c1', 'Groceries', 0),
      category('c2', 'Rent', 1),
      category('c3', 'Transport', 2),
      category('c4', 'Fun', 3),
    ],
  },
]

function allocation(overrides: Partial<AllocationDetail> & { categoryId: string }): AllocationDetail {
  return {
    amount: 0,
    rollover: 0,
    spending: 0,
    reserved: 0,
    remaining: 0,
    status: 'on_track',
    ...overrides,
  }
}

// The page opens on the real current month; fixtures must carry the same
// (year, month) or mutation responses would land under a different query key.
const today = new Date()

function periodDetail(overrides: Partial<BudgetPeriodDetail> = {}): BudgetPeriodDetail {
  return {
    id: 'p1',
    year: today.getFullYear(),
    month: today.getMonth() + 1,
    currency: 'UAH',
    plannedIncome: 100000,
    notes: '',
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
    unallocated: 50000,
    categories: [
      allocation({ categoryId: 'c1', amount: 20000, spending: 5000, remaining: 15000 }),
      allocation({ categoryId: 'c2', amount: 30000, spending: 30000, remaining: 0, status: 'approaching_limit' }),
    ],
    ...overrides,
  }
}

// Routes the page's API traffic: categories, the month lookup, allocation
// writes (recorded and answered via nextDetail), and period creation.
function setupFetch({
  detail,
  nextDetail,
  monthMissing = false,
}: {
  detail?: BudgetPeriodDetail
  nextDetail?: (categoryId: string, amount: number) => BudgetPeriodDetail
  monthMissing?: boolean
} = {}) {
  const allocationCalls: { categoryId: string; amount: number }[] = []
  const createCalls: unknown[] = []
  const fetchMock = mockFetch((url, init) => {
    if (url.startsWith('/api/categories')) {
      return jsonResponse(200, { groups })
    }
    if (url.includes('/allocations/')) {
      const categoryId = url.split('/allocations/')[1]
      const { amount } = JSON.parse(String(init?.body)) as { amount: number }
      allocationCalls.push({ categoryId, amount })
      return jsonResponse(200, { period: nextDetail ? nextDetail(categoryId, amount) : detail })
    }
    if (url.includes('/api/budgets/month/')) {
      return monthMissing
        ? jsonResponse(404, { error: 'budget period not found' })
        : jsonResponse(200, { period: detail })
    }
    if (url === '/api/budgets' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { year: number; month: number }
      createCalls.push(JSON.parse(String(init.body)))
      // Echo the requested month so the created period matches the view.
      return jsonResponse(201, { period: { ...periodDetail(), year: body.year, month: body.month } })
    }
    return jsonResponse(404, { error: 'not found' })
  })
  return { fetchMock, allocationCalls, createCalls }
}

describe('BudgetPage', () => {
  it('shows the header math and updates unallocated after editing an allocation', async () => {
    setupFetch({
      detail: periodDetail(),
      nextDetail: (_categoryId, amount) =>
        periodDetail({
          unallocated: 40000,
          categories: [
            allocation({ categoryId: 'c1', amount, spending: 5000, remaining: amount - 5000 }),
            allocation({ categoryId: 'c2', amount: 30000, spending: 30000, remaining: 0 }),
          ],
        }),
    })
    renderWithProviders(<BudgetPage />)

    const available = await screen.findByTestId('available-to-assign')
    expect(available).toHaveTextContent('500.00')

    await userEvent.click(screen.getByRole('button', { name: 'Edit budgeted amount for Groceries' }))
    const input = screen.getByLabelText('Budgeted amount for Groceries')
    expect(input).toHaveValue('200.00')
    await userEvent.clear(input)
    await userEvent.type(input, '300.00{Enter}')

    expect(await screen.findByTestId('available-to-assign')).toHaveTextContent('400.00')
  })

  it('rejects invalid allocation input without calling the API', async () => {
    const { allocationCalls } = setupFetch({ detail: periodDetail() })
    renderWithProviders(<BudgetPage />)

    await userEvent.click(
      await screen.findByRole('button', { name: 'Edit budgeted amount for Groceries' }),
    )
    const input = screen.getByLabelText('Budgeted amount for Groceries')
    await userEvent.clear(input)
    await userEvent.type(input, 'abc{Enter}')

    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(allocationCalls).toHaveLength(0)
  })

  it('renders a status badge per state with icon and text', async () => {
    setupFetch({
      detail: periodDetail({
        categories: [
          allocation({ categoryId: 'c1', amount: 20000, remaining: 15000, status: 'on_track' }),
          allocation({ categoryId: 'c2', amount: 30000, remaining: 4000, status: 'approaching_limit' }),
          allocation({ categoryId: 'c3', amount: 10000, remaining: -2500, status: 'over_budget' }),
          allocation({ categoryId: 'c4', amount: 0, spending: 1000, remaining: -1000, status: 'unfunded' }),
        ],
      }),
    })
    renderWithProviders(<BudgetPage />)

    expect(await screen.findByText('On track')).toBeInTheDocument()
    expect(screen.getByText('Approaching limit')).toBeInTheDocument()
    expect(screen.getByText('Over budget')).toBeInTheDocument()
    expect(screen.getByText('Unfunded')).toBeInTheDocument()
    // The over-budget row offers a suggested action.
    expect(screen.getByRole('button', { name: 'Cover overspending' })).toBeInTheDocument()
  })

  it('moves money between categories as two allocation writes', async () => {
    const { allocationCalls } = setupFetch({
      detail: periodDetail(),
      nextDetail: (categoryId, amount) => {
        const base = periodDetail()
        return periodDetail({
          categories: base.categories.map((c) =>
            c.categoryId === categoryId ? { ...c, amount } : c,
          ),
        })
      },
    })
    renderWithProviders(<BudgetPage />)

    await userEvent.click(await screen.findByRole('button', { name: 'Move money' }))
    const dialog = screen.getByRole('dialog', { name: 'Move money' })
    await userEvent.selectOptions(within(dialog).getByLabelText('Move from'), 'c1')
    await userEvent.selectOptions(within(dialog).getByLabelText('Move to'), 'c2')
    await userEvent.type(within(dialog).getByLabelText('Amount'), '50.00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Move' }))

    expect(allocationCalls).toEqual([
      { categoryId: 'c1', amount: 15000 },
      { categoryId: 'c2', amount: 35000 },
    ])
    expect(screen.queryByRole('dialog', { name: 'Move money' })).not.toBeInTheDocument()
  })

  it('refuses to move more than the source category has budgeted', async () => {
    const { allocationCalls } = setupFetch({ detail: periodDetail() })
    renderWithProviders(<BudgetPage />)

    await userEvent.click(await screen.findByRole('button', { name: 'Move money' }))
    const dialog = screen.getByRole('dialog', { name: 'Move money' })
    await userEvent.selectOptions(within(dialog).getByLabelText('Move from'), 'c1')
    await userEvent.selectOptions(within(dialog).getByLabelText('Move to'), 'c2')
    await userEvent.type(within(dialog).getByLabelText('Amount'), '999.00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Move' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('at most')
    expect(allocationCalls).toHaveLength(0)
  })

  it('prefills the move dialog from the cover-overspending action', async () => {
    setupFetch({
      detail: periodDetail({
        categories: [
          allocation({ categoryId: 'c1', amount: 20000, remaining: 15000 }),
          allocation({ categoryId: 'c2', amount: 10000, spending: 12500, remaining: -2500, status: 'over_budget' }),
        ],
      }),
    })
    renderWithProviders(<BudgetPage />)

    await userEvent.click(await screen.findByRole('button', { name: 'Cover overspending' }))
    const dialog = screen.getByRole('dialog', { name: 'Move money' })
    expect(within(dialog).getByLabelText('Move to')).toHaveValue('c2')
    expect(within(dialog).getByLabelText('Amount')).toHaveValue('25.00')
  })

  it('offers to create a missing month empty or copied from the prior month', async () => {
    const { createCalls } = setupFetch({ monthMissing: true })
    renderWithProviders(<BudgetPage />)

    expect(await screen.findByText(/No budget for/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Copy last month' }))

    const now = new Date()
    expect(createCalls).toEqual([
      {
        year: now.getFullYear(),
        month: now.getMonth() + 1,
        copyPrior: true,
        plannedIncome: 0,
        notes: '',
      },
    ])
    // The created period replaces the empty state.
    expect(await screen.findByText('Available to assign')).toBeInTheDocument()
  })

  it('collapses the table to stacked cards on mobile', async () => {
    setupFetch({ detail: periodDetail() })
    mockViewport(false)
    renderWithProviders(<BudgetPage />)

    expect(await screen.findByText('Groceries')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Essentials' })).toBeInTheDocument()
  })
})
