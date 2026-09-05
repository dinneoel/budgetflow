import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { mockFetch, renderWithProviders } from '../../test/utils'
import { DataSection } from './DataSection'

function renderDataSection() {
  return renderWithProviders(
    <Routes>
      <Route path="/settings" element={<DataSection />} />
      <Route path="/sign-in" element={<div>sign-in page</div>} />
    </Routes>,
    { route: '/settings' },
  )
}

describe('DataSection', () => {
  it('links to the full data export', () => {
    mockFetch(() => new Response(null, { status: 204 }))
    renderDataSection()

    expect(screen.getByRole('link', { name: 'Export all data' })).toHaveAttribute(
      'href',
      '/api/export/all.zip',
    )
  })

  it('gates deletion behind the acknowledgement and password', async () => {
    const fetchMock = mockFetch(() => new Response(null, { status: 204 }))
    renderDataSection()

    await userEvent.click(screen.getByRole('button', { name: 'Delete account…' }))
    const deleteButton = screen.getByRole('button', { name: 'Permanently delete my account' })
    expect(deleteButton).toBeDisabled()

    // Password alone is not enough.
    await userEvent.type(screen.getByLabelText('Confirm your password'), 'hunter2-hunter2')
    expect(deleteButton).toBeDisabled()
    expect(fetchMock).not.toHaveBeenCalled()

    await userEvent.click(
      screen.getByLabelText('I understand this permanently deletes all my data'),
    )
    expect(deleteButton).toBeEnabled()

    await userEvent.click(deleteButton)
    expect(await screen.findByText('sign-in page')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/account/delete',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ password: 'hunter2-hunter2' }),
      }),
    )
  })

  it('surfaces a wrong-password error and stays on the page', async () => {
    mockFetch(() =>
      new Response(JSON.stringify({ error: 'password is incorrect' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    renderDataSection()

    await userEvent.click(screen.getByRole('button', { name: 'Delete account…' }))
    await userEvent.click(
      screen.getByLabelText('I understand this permanently deletes all my data'),
    )
    await userEvent.type(screen.getByLabelText('Confirm your password'), 'wrong-password')
    await userEvent.click(screen.getByRole('button', { name: 'Permanently delete my account' }))

    expect(await screen.findByText('password is incorrect')).toBeInTheDocument()
    expect(screen.queryByText('sign-in page')).not.toBeInTheDocument()
  })
})
