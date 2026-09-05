import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { SignUpPage } from './SignUpPage'

function renderSignUp() {
  return renderWithProviders(
    <Routes>
      <Route path="/sign-up" element={<SignUpPage />} />
      <Route path="/" element={<div>dashboard home</div>} />
    </Routes>,
    { route: '/sign-up' },
  )
}

describe('SignUpPage', () => {
  it('validates name, email, password length, and currency format', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, {}))
    renderSignUp()

    await userEvent.type(screen.getByLabelText('Email'), 'bad-email')
    await userEvent.type(screen.getByLabelText('Password'), 'short')
    await userEvent.clear(screen.getByLabelText('Default currency'))
    await userEvent.type(screen.getByLabelText('Default currency'), 'X1')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('Name is required')).toBeInTheDocument()
    expect(screen.getByText('Enter a valid email address')).toBeInTheDocument()
    expect(screen.getByText('Password must be at least 8 characters')).toBeInTheDocument()
    expect(screen.getByText('Use a 3-letter currency code (e.g. USD)')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('creates the account and navigates home on success', async () => {
    const fetchMock = mockFetch(() =>
      jsonResponse(201, { user: testUser, csrfToken: 'fresh-csrf' }),
    )
    renderSignUp()

    await userEvent.type(screen.getByLabelText('Name'), 'Leonid')
    await userEvent.type(screen.getByLabelText('Email'), 'leonid@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'long-enough-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('dashboard home')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/sign-up', expect.anything())
  })
})
