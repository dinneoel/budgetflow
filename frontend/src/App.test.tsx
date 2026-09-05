import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from './App'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from './test/utils'

describe('App', () => {
  it('sends unauthenticated visitors to the sign-in page', async () => {
    mockFetch(() => jsonResponse(401, { error: 'unauthenticated' }))
    renderWithProviders(<App />, { route: '/' })

    expect(
      await screen.findByRole('heading', { name: 'Sign in to your account' }),
    ).toBeInTheDocument()
  })

  it('shows the dashboard empty state for a signed-in user', async () => {
    mockFetch(() => jsonResponse(200, { user: testUser, csrfToken: 'csrf' }))
    renderWithProviders(<App />, { route: '/' })

    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.getByText('Welcome to BudgetFlow')).toBeInTheDocument()
  })
})
