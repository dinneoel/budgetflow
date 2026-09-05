import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Account } from '../../api/accounts'
import { jsonResponse, mockFetch, renderWithProviders } from '../../test/utils'
import { AccountDetailPage } from './AccountDetailPage'

const account: Account = {
  id: 'a0000000-0000-4000-8000-000000000001',
  name: 'Main checking',
  institution: 'Mono',
  type: 'checking',
  currency: 'USD',
  openingBalance: 100_000,
  balance: 125_000,
  includeInNetWorth: true,
  archivedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

function renderDetail() {
  return renderWithProviders(
    <Routes>
      <Route path="/accounts/:accountId" element={<AccountDetailPage />} />
    </Routes>,
    { route: `/accounts/${account.id}` },
  )
}

describe('AccountDetailPage', () => {
  it('shows the account header with type, institution, and balance', async () => {
    mockFetch(() => jsonResponse(200, { account }))
    renderDetail()

    expect(await screen.findByRole('heading', { name: 'Main checking' })).toBeInTheDocument()
    expect(screen.getByText('Checking · Mono')).toBeInTheDocument()
    expect(screen.getByText('$1,250.00')).toBeInTheDocument()
  })

  it('reconciles against a statement balance and reports the adjustment', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/accounts/${account.id}/reconcile` && init?.method === 'POST') {
        return jsonResponse(200, {
          account: { ...account, balance: 120_000 },
          adjustment: { id: 't1', type: 'adjustment', amount: -5_000, date: '2026-09-05', payee: '', notes: '' },
        })
      }
      return jsonResponse(200, { account })
    })
    renderDetail()

    await userEvent.type(await screen.findByLabelText('Statement balance'), '1200.00')
    await userEvent.click(screen.getByRole('button', { name: 'Reconcile' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Adjustment of -$50.00 recorded. New balance: $1,200.00.',
    )
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/reconcile'))
    expect(JSON.parse(String(call![1]!.body))).toEqual({ statementBalance: 120_000 })
  })

  it('rejects an unparseable statement amount without calling the API', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, { account }))
    renderDetail()

    await userEvent.type(await screen.findByLabelText('Statement balance'), 'twelve')
    await userEvent.click(screen.getByRole('button', { name: 'Reconcile' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Enter an amount like 1250.00')
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/reconcile'))).toBe(false)
  })

  it('archives the account from the detail page', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/accounts/${account.id}/archive` && init?.method === 'POST') {
        return jsonResponse(200, { account: { ...account, archivedAt: '2026-09-05T00:00:00Z' } })
      }
      return jsonResponse(200, { account })
    })
    renderDetail()

    await userEvent.click(await screen.findByRole('button', { name: 'Archive account' }))

    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/archive'))).toBe(true)
  })

  it('shows an error state for an unknown account', async () => {
    mockFetch(() => jsonResponse(404, { error: 'account not found' }))
    renderDetail()

    expect(await screen.findByRole('alert')).toHaveTextContent('account not found')
    expect(screen.getByRole('link', { name: 'Back to accounts' })).toBeInTheDocument()
  })
})
