import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { ImportBatch } from '../../api/imports'
import { runAxe } from '../../test/axe'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { ImportPage } from './ImportPage'

const batchId = 'b0000000-0000-4000-8000-000000000001'

const pendingBatch: ImportBatch = {
  id: batchId,
  fileName: 'bank.csv',
  status: 'pending',
  rowCount: 3,
  accountId: null,
  createdAt: '2026-09-05T00:00:00Z',
  committedAt: null,
}

const uploadResult = {
  batch: pendingBatch,
  columns: ['Date', 'Amount', 'Description'],
  hasHeader: true,
  rowCount: 3,
  sampleRows: [
    ['2026-09-01', '-42.50', 'Silpo'],
    ['2026-09-02', '-10.00', 'Coffee'],
  ],
  dateFormats: ['auto', 'YYYY-MM-DD', 'DD.MM.YYYY'],
  amountFormats: ['auto', 'dot_decimal', 'comma_decimal'],
}

const previewResult = {
  rows: [
    {
      index: 0,
      raw: ['2026-09-01', '-42.50', 'Silpo'],
      date: '2026-09-01',
      amount: -4250,
      type: 'expense',
      payee: 'Silpo',
      notes: '',
      errors: [],
      warnings: [],
      duplicateOf: null,
    },
    {
      index: 1,
      raw: ['not-a-date', '-10.00', 'Coffee'],
      payee: 'Coffee',
      notes: '',
      errors: ['could not parse date "not-a-date"'],
      warnings: [],
      duplicateOf: null,
    },
    {
      index: 2,
      raw: ['2026-09-03', '-15.00', 'Metro'],
      date: '2026-09-03',
      amount: -1500,
      type: 'expense',
      payee: 'Metro',
      notes: '',
      errors: [],
      warnings: ['possible duplicate'],
      duplicateOf: ['t0000000-0000-4000-8000-000000000009'],
    },
  ],
  valid: 2,
  errored: 1,
  duplicates: 1,
}

const account = {
  id: 'a0000000-0000-4000-8000-000000000001',
  name: 'Main checking',
  institution: '',
  type: 'checking',
  currency: 'UAH',
  openingBalance: 0,
  balance: 100000,
  includeInNetWorth: true,
  archivedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

function wizardHandler(batches: ImportBatch[] = []) {
  return (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    if (url === '/api/imports' && method === 'POST') {
      return jsonResponse(201, uploadResult)
    }
    if (url === '/api/imports' && method === 'GET') {
      return jsonResponse(200, { batches })
    }
    if (url === `/api/imports/${batchId}/mapping` && method === 'PUT') {
      return jsonResponse(200, { batch: pendingBatch, mapping: JSON.parse(String(init!.body)) })
    }
    if (url === `/api/imports/${batchId}/preview` && method === 'GET') {
      return jsonResponse(200, previewResult)
    }
    if (url === `/api/imports/${batchId}/commit` && method === 'POST') {
      return jsonResponse(201, {
        batch: { ...pendingBatch, status: 'committed' },
        createdIds: ['t1', 't2'],
        skippedErrors: 1,
        skippedDuplicates: 1,
        skippedManually: 0,
      })
    }
    if (url === `/api/imports/${batchId}` && method === 'DELETE') {
      return jsonResponse(200, { batch: pendingBatch, deletedTransactionIds: ['t1', 't2'] })
    }
    if (url.startsWith('/api/accounts')) {
      return jsonResponse(200, { accounts: [account] })
    }
    if (url.startsWith('/api/auth/me')) {
      return jsonResponse(200, { user: testUser, csrfToken: 'csrf' })
    }
    return jsonResponse(404, { error: `unhandled ${method} ${url}` })
  }
}

function renderImport(batches: ImportBatch[] = []) {
  const fetchMock = mockFetch(wizardHandler(batches))
  renderWithProviders(
    <Routes>
      <Route path="/import" element={<ImportPage />} />
    </Routes>,
    { route: '/import' },
  )
  return fetchMock
}

async function uploadFile() {
  const file = new File(['Date,Amount,Description\n2026-09-01,-42.50,Silpo\n'], 'bank.csv', {
    type: 'text/csv',
  })
  await userEvent.upload(screen.getByLabelText('CSV file'), file)
}

describe('ImportPage', () => {
  it('walks upload → mapping (with validation errors) → preview → commit', async () => {
    const fetchMock = renderImport()

    // Step 1: upload.
    await uploadFile()

    // Step 2: mapping shows detected columns and sample rows.
    expect(await screen.findByText('Map columns from bank.csv')).toBeInTheDocument()
    expect(screen.getByText('3 rows detected (first row looks like a header).')).toBeInTheDocument()
    expect(screen.getByText('Silpo')).toBeInTheDocument()

    // Submitting without required choices surfaces validation errors and
    // does not call the server.
    await userEvent.click(screen.getByRole('button', { name: 'Continue to preview' }))
    expect(
      screen.getByText('Choose the account these transactions belong to'),
    ).toBeInTheDocument()
    expect(screen.getByText('Choose which column holds the date')).toBeInTheDocument()
    expect(screen.getByText('Choose which column holds the amount')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.find(([url]) => String(url).includes('/mapping')),
    ).toBeUndefined()

    // Fill the mapping and continue.
    await userEvent.selectOptions(screen.getByLabelText('Import into account'), account.id)
    await userEvent.selectOptions(screen.getByLabelText('Date column'), '0')
    await userEvent.selectOptions(screen.getByLabelText('Amount column'), '1')
    await userEvent.selectOptions(screen.getByLabelText('Payee column'), '2')
    await userEvent.click(screen.getByRole('button', { name: 'Continue to preview' }))

    const mappingCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `/api/imports/${batchId}/mapping` && init?.method === 'PUT',
    )
    expect(mappingCall).toBeDefined()
    expect(JSON.parse(String(mappingCall![1]!.body))).toEqual({
      accountId: account.id,
      dateColumn: 0,
      amountColumn: 1,
      payeeColumn: 2,
      notesColumn: null,
      categoryColumn: null,
      dateFormat: 'auto',
      amountFormat: 'auto',
    })

    // Step 3: preview shows counts, per-row errors, and duplicate flags.
    const preview = await screen.findByRole('region', { name: 'Preview' })
    expect(
      within(preview).getByText('2 rows ready · 1 with errors (skipped) · 1 possible duplicates'),
    ).toBeInTheDocument()
    expect(within(preview).getByText('could not parse date "not-a-date"')).toBeInTheDocument()
    expect(within(preview).getByText('Possible duplicate')).toBeInTheDocument()

    // Commit without including duplicates.
    await userEvent.click(within(preview).getByRole('button', { name: 'Import 2 transactions' }))
    const commitCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `/api/imports/${batchId}/commit` && init?.method === 'POST',
    )
    expect(commitCall).toBeDefined()
    expect(JSON.parse(String(commitCall![1]!.body))).toEqual({
      includeDuplicates: false,
      skipRows: [],
    })

    // Step 4: summary of what was created and skipped.
    expect(await screen.findByText('Imported 2 transactions')).toBeInTheDocument()
    expect(screen.getByText('1 rows skipped because of errors.')).toBeInTheDocument()
    expect(screen.getByText('1 duplicates skipped.')).toBeInTheDocument()
  })

  it('lists previous imports and deletes a batch after confirmation', async () => {
    const committed: ImportBatch = {
      ...pendingBatch,
      status: 'committed',
      committedAt: '2026-09-04T00:00:00Z',
    }
    const fetchMock = renderImport([committed])

    const list = await screen.findByRole('region', { name: 'Previous imports' })
    expect(within(list).getByText('bank.csv')).toBeInTheDocument()

    // Deleting requires an explicit confirmation click.
    await userEvent.click(within(list).getByRole('button', { name: 'Delete import' }))
    expect(
      fetchMock.mock.calls.find(([, init]) => init?.method === 'DELETE'),
    ).toBeUndefined()
    await userEvent.click(within(list).getByRole('button', { name: 'Confirm delete' }))

    const deleteCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `/api/imports/${batchId}` && init?.method === 'DELETE',
    )
    expect(deleteCall).toBeDefined()
  })

  it('has no axe violations on the upload and mapping steps', async () => {
    renderImport()
    const container = document.body
    await screen.findByLabelText('CSV file')
    expect(await runAxe(container)).toHaveNoViolations()

    await uploadFile()
    await screen.findByRole('region', { name: 'Map columns' })
    expect(await runAxe(container)).toHaveNoViolations()
  })
})
