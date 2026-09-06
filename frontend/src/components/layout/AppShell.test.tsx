import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { jsonResponse, mockFetch, mockViewport, renderWithProviders, testUser } from '../../test/utils'
import { AppShell } from './AppShell'

const navLabels = ['Dashboard', 'Budget', 'Transactions', 'Goals', 'Reports']

function renderShell() {
  mockFetch(() => jsonResponse(200, { user: testUser, csrfToken: 'csrf' }))
  return renderWithProviders(
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/" element={<div>page body</div>} />
        <Route path="/categories" element={<div>categories body</div>} />
      </Route>
    </Routes>,
  )
}

describe('AppShell', () => {
  beforeEach(() => {
    vi.stubGlobal('scrollTo', vi.fn())
  })

  it('returns to the top when navigating to another page', async () => {
    mockViewport(true)
    renderShell()
    const user = userEvent.setup()
    vi.mocked(window.scrollTo).mockClear()

    await user.click(screen.getByRole('link', { name: 'Categories' }))

    expect(screen.getByText('categories body')).toBeInTheDocument()
    expect(window.scrollTo).toHaveBeenCalledWith(0, 0)
  })

  it('renders the sidebar navigation with all primary links on desktop', async () => {
    mockViewport(true)
    renderShell()

    const nav = screen.getByRole('navigation', { name: 'Primary' })
    for (const label of navLabels) {
      expect(within(nav).getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Add transaction' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /Sign out/ })).toBeInTheDocument()
    expect(screen.getByText('page body')).toBeInTheDocument()
  })

  it('renders the bottom navigation and quick-add button on mobile', () => {
    mockViewport(false)
    renderShell()

    const nav = screen.getByRole('navigation', { name: 'Primary' })
    for (const label of navLabels) {
      expect(within(nav).getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByRole('link', { name: 'Add transaction' })).toBeInTheDocument()
    // The desktop-only sign-out button lives in the sidebar, absent on mobile.
    expect(screen.queryByRole('button', { name: /Sign out/ })).not.toBeInTheDocument()
  })
})
