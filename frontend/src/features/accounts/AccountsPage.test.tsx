import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Account } from '../../api/accounts'
import { jsonResponse, mockFetch, renderWithProviders } from '../../test/utils'
import { AccountsPage } from './AccountsPage'

const wallet: Account = {
  id: 'a0000000-0000-4000-8000-000000000001',
  name: 'Wallet',
  institution: '',
  type: 'cash',
  currency: 'USD',
  openingBalance: 5000,
  balance: 12550,
  includeInNetWorth: true,
  archivedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

function renderAccounts() {
  return renderWithProviders(
    <Routes>
      <Route path="/accounts" element={<AccountsPage />} />
    </Routes>,
    { route: '/accounts' },
  )
}

describe('AccountsPage', () => {
  it('lists accounts with formatted balances', async () => {
    mockFetch(() => jsonResponse(200, { accounts: [wallet] }))
    renderAccounts()

    expect(await screen.findByText('Wallet')).toBeInTheDocument()
    expect(screen.getByText('$125.50')).toBeInTheDocument()
  })

  it('creates an account, sending the opening balance in minor units', async () => {
    const created: Account[] = []
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/accounts' && init?.method === 'POST') {
        const body = JSON.parse(String(init.body)) as Account
        created.push({ ...wallet, ...body, id: 'a0000000-0000-4000-8000-000000000002' })
        return jsonResponse(201, { account: created[0] })
      }
      return jsonResponse(200, { accounts: created })
    })
    renderAccounts()

    await userEvent.click(await screen.findByRole('button', { name: 'Add your first account' }))
    await userEvent.type(screen.getByLabelText('Account name'), 'Main checking')
    await userEvent.selectOptions(screen.getByLabelText('Type'), 'checking')
    await userEvent.type(screen.getByLabelText('Currency'), 'UAH')
    await userEvent.clear(screen.getByLabelText('Opening balance'))
    await userEvent.type(screen.getByLabelText('Opening balance'), '1500.25')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('Main checking')).toBeInTheDocument()
    const createCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(createCall).toBeDefined()
    expect(JSON.parse(String(createCall![1]!.body))).toEqual({
      name: 'Main checking',
      institution: '',
      type: 'checking',
      currency: 'UAH',
      openingBalance: 150025,
      includeInNetWorth: true,
    })
  })

  it('rejects a malformed opening balance before calling the API', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, { accounts: [] }))
    renderAccounts()

    await userEvent.click(await screen.findByRole('button', { name: 'Add your first account' }))
    await userEvent.type(screen.getByLabelText('Account name'), 'Broken')
    await userEvent.type(screen.getByLabelText('Currency'), 'USD')
    await userEvent.clear(screen.getByLabelText('Opening balance'))
    await userEvent.type(screen.getByLabelText('Opening balance'), 'not-money')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('Enter an amount like 1250.00')).toBeInTheDocument()
    const posts = fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')
    expect(posts).toHaveLength(0)
  })
})
