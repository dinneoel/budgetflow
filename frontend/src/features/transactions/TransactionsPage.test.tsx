import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { Transaction } from '../../api/transactions'
import { runAxe } from '../../test/axe'
import { jsonResponse, mockFetch, mockViewport, renderWithProviders } from '../../test/utils'
import { TransactionsPage } from './TransactionsPage'

function todayString() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
    d.getDate(),
  ).padStart(2, '0')}`
}

const accounts = [
  {
    id: 'a1',
    name: 'Checking',
    institution: '',
    type: 'checking',
    currency: 'UAH',
    openingBalance: 0,
    balance: 100000,
    includeInNetWorth: true,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
  },
  {
    id: 'a2',
    name: 'Savings',
    institution: '',
    type: 'savings',
    currency: 'UAH',
    openingBalance: 0,
    balance: 500000,
    includeInNetWorth: true,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
  },
]

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
    categories: [category('c1', 'Groceries', 0), category('c2', 'Rent', 1)],
  },
]

function tx(overrides: Partial<Transaction> = {}): Transaction {
  return {
    id: 't1',
    accountId: 'a1',
    categoryId: 'c1',
    type: 'expense',
    status: 'uncleared',
    amount: -1250,
    date: '2026-09-04',
    payee: 'Silpo',
    notes: '',
    reviewed: false,
    transferPairId: null,
    deletedAt: null,
    createdAt: '2026-09-04T00:00:00Z',
    updatedAt: '2026-09-04T00:00:00Z',
    splits: [],
    tags: [],
    ...overrides,
  }
}

const defaultRows = [tx(), tx({ id: 't2', payee: 'ATB', amount: -3400, categoryId: 'c2' })]
const deletedRow = tx({ id: 't9', payee: 'Old coffee', deletedAt: '2026-09-03T00:00:00Z' })

// Routes the page's API traffic and records mutating calls for assertions.
function setupFetch({
  rows = defaultRows,
  createResponse,
}: { rows?: Transaction[]; createResponse?: Transaction } = {}) {
  const listCalls: string[] = []
  const createCalls: unknown[] = []
  const transferCalls: unknown[] = []
  const bulkCalls: unknown[] = []
  const restoreCalls: string[] = []
  const fetchMock = mockFetch((url, init) => {
    if (url.startsWith('/api/accounts')) {
      return jsonResponse(200, { accounts })
    }
    if (url.startsWith('/api/categories')) {
      return jsonResponse(200, { groups })
    }
    if (url === '/api/transactions/transfer' && init?.method === 'POST') {
      transferCalls.push(JSON.parse(String(init.body)))
      return jsonResponse(201, {
        outTransaction: tx({ id: 'out1', type: 'transfer', categoryId: null, amount: -2500 }),
        inTransaction: tx({ id: 'in1', type: 'transfer', categoryId: null, amount: 2500 }),
      })
    }
    if (url === '/api/transactions/bulk' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { action: string; ids: string[] }
      bulkCalls.push(body)
      return jsonResponse(200, { action: body.action, affectedIds: body.ids })
    }
    if (/\/api\/transactions\/[^/]+\/restore$/.test(url) && init?.method === 'POST') {
      restoreCalls.push(url)
      return jsonResponse(200, { transaction: tx({ id: 't9', deletedAt: null }) })
    }
    if (url === '/api/transactions' && init?.method === 'POST') {
      createCalls.push(JSON.parse(String(init.body)))
      return jsonResponse(201, { transaction: createResponse ?? tx({ id: 'new1' }) })
    }
    if (url.startsWith('/api/transactions')) {
      listCalls.push(url)
      const deleted = url.includes('deleted=true')
      const pageRows = deleted ? [deletedRow] : rows
      return jsonResponse(200, {
        transactions: pageRows,
        total: pageRows.length,
        limit: 50,
        offset: 0,
      })
    }
    return jsonResponse(404, { error: 'not found' })
  })
  return { fetchMock, listCalls, createCalls, transferCalls, bulkCalls, restoreCalls }
}

async function openEditor() {
  await userEvent.click(await screen.findByRole('button', { name: 'Add transaction' }))
  return screen.getByRole('dialog', { name: 'Add transaction' })
}

describe('TransactionsPage', () => {
  it('quick-adds an expense with today as the default date', async () => {
    const { createCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    const dialog = await openEditor()
    expect(within(dialog).getByLabelText('Date')).toHaveValue(todayString())
    await userEvent.type(within(dialog).getByLabelText('Amount'), '12.50')
    await userEvent.type(within(dialog).getByLabelText('Payee'), 'Coffee shop')
    await userEvent.selectOptions(within(dialog).getByLabelText('Category'), 'c1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createCalls).toHaveLength(1))
    expect(createCalls[0]).toMatchObject({
      accountId: 'a1',
      categoryId: 'c1',
      type: 'expense',
      amount: 1250,
      date: todayString(),
      payee: 'Coffee shop',
      splits: [],
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('surfaces a duplicate warning after entry', async () => {
    setupFetch({
      createResponse: tx({
        id: 'new1',
        payee: 'Coffee shop',
        duplicateWarning: true,
        duplicateOf: ['t1'],
      }),
    })
    renderWithProviders(<TransactionsPage />)

    const dialog = await openEditor()
    await userEvent.type(within(dialog).getByLabelText('Amount'), '12.50')
    await userEvent.type(within(dialog).getByLabelText('Payee'), 'Coffee shop')
    await userEvent.selectOptions(within(dialog).getByLabelText('Category'), 'c1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    const banner = await screen.findByRole('status')
    expect(banner).toHaveTextContent('possible duplicate')
    expect(banner).toHaveTextContent('Coffee shop')
  })

  it('blocks saving splits that do not sum to the amount, then saves once balanced', async () => {
    const { createCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    const dialog = await openEditor()
    await userEvent.type(within(dialog).getByLabelText('Amount'), '100.00')
    await userEvent.type(within(dialog).getByLabelText('Payee'), 'Market')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Split across categories' }))

    await userEvent.selectOptions(within(dialog).getByLabelText('Split 1 category'), 'c1')
    await userEvent.type(within(dialog).getByLabelText('Split 1 amount'), '40.00')
    await userEvent.selectOptions(within(dialog).getByLabelText('Split 2 category'), 'c2')
    await userEvent.type(within(dialog).getByLabelText('Split 2 amount'), '50.00')
    expect(within(dialog).getByTestId('split-remainder')).toHaveTextContent('10.00')

    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('add up')
    expect(createCalls).toHaveLength(0)

    const split2 = within(dialog).getByLabelText('Split 2 amount')
    await userEvent.clear(split2)
    await userEvent.type(split2, '60.00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createCalls).toHaveLength(1))
    expect(createCalls[0]).toMatchObject({
      categoryId: null,
      amount: 10000,
      splits: [
        { categoryId: 'c1', amount: 4000, memo: '' },
        { categoryId: 'c2', amount: 6000, memo: '' },
      ],
    })
  })

  it('creates a transfer between two accounts', async () => {
    const { transferCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    const dialog = await openEditor()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Transfer' }))
    await userEvent.type(within(dialog).getByLabelText('Amount'), '25.00')
    await userEvent.selectOptions(within(dialog).getByLabelText('From account'), 'a1')
    await userEvent.selectOptions(within(dialog).getByLabelText('To account'), 'a2')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(transferCalls).toHaveLength(1))
    expect(transferCalls[0]).toEqual({
      fromAccountId: 'a1',
      toAccountId: 'a2',
      amount: 2500,
      date: todayString(),
      notes: '',
    })
  })

  it('applies filters to the list request', async () => {
    const { listCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    expect(await screen.findByText('Silpo')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Search'), 'coffee')
    await userEvent.selectOptions(screen.getByLabelText('Account'), 'a1')
    await userEvent.click(screen.getByRole('button', { name: 'Apply filters' }))

    await waitFor(() => {
      const last = listCalls.at(-1) ?? ''
      expect(last).toContain('q=coffee')
      expect(last).toContain('accountId=a1')
    })
  })

  it('bulk-categorizes the selected transactions', async () => {
    const { bulkCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    await userEvent.click(await screen.findByRole('checkbox', { name: 'Select Silpo' }))
    await userEvent.click(screen.getByRole('checkbox', { name: 'Select ATB' }))

    const toolbar = screen.getByRole('toolbar', { name: 'Bulk actions' })
    expect(within(toolbar).getByText('2 selected')).toBeInTheDocument()
    await userEvent.selectOptions(within(toolbar).getByLabelText('Bulk category'), 'c1')
    await userEvent.click(within(toolbar).getByRole('button', { name: 'Categorize' }))

    await waitFor(() => expect(bulkCalls).toHaveLength(1))
    expect(bulkCalls[0]).toMatchObject({
      action: 'categorize',
      ids: ['t1', 't2'],
      categoryId: 'c1',
    })
    // The toolbar clears once the operation lands.
    await waitFor(() =>
      expect(screen.queryByRole('toolbar', { name: 'Bulk actions' })).not.toBeInTheDocument(),
    )
  })

  it('shows soft-deleted transactions and restores one', async () => {
    const { listCalls, restoreCalls } = setupFetch()
    renderWithProviders(<TransactionsPage />)

    await userEvent.click(await screen.findByRole('button', { name: 'View deleted' }))
    await waitFor(() => expect(listCalls.at(-1)).toContain('deleted=true'))

    expect(await screen.findByText('Old coffee')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Restore Old coffee' }))

    await waitFor(() => expect(restoreCalls).toHaveLength(1))
    expect(restoreCalls[0]).toContain('/api/transactions/t9/restore')
  })

  it('collapses the table to stacked cards on mobile', async () => {
    setupFetch()
    mockViewport(false)
    renderWithProviders(<TransactionsPage />)

    expect(await screen.findByText('Silpo')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    // Card rows keep selection and actions available.
    expect(screen.getByRole('checkbox', { name: 'Select Silpo' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit Silpo' })).toBeInTheDocument()
  })

  it('has no axe violations on the list and in the open editor dialog', async () => {
    setupFetch()
    const { container } = renderWithProviders(<TransactionsPage />)

    await screen.findByRole('button', { name: 'Add transaction' })
    expect(await runAxe(container)).toHaveNoViolations()

    await openEditor()
    expect(await runAxe(container)).toHaveNoViolations()
  })
})
