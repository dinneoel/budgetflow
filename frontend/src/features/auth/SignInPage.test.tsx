import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { SignInPage } from './SignInPage'

function renderSignIn() {
  return renderWithProviders(
    <Routes>
      <Route path="/sign-in" element={<SignInPage />} />
      <Route path="/" element={<div>dashboard home</div>} />
    </Routes>,
    { route: '/sign-in' },
  )
}

describe('SignInPage', () => {
  it('shows validation errors and does not call the API on invalid input', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, {}))
    renderSignIn()

    await userEvent.type(screen.getByLabelText('Email'), 'not-an-email')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Enter a valid email address')).toBeInTheDocument()
    expect(screen.getByText('Password is required')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('surfaces the server error message on rejected credentials', async () => {
    mockFetch(() => jsonResponse(401, { error: 'invalid email or password' }))
    renderSignIn()

    await userEvent.type(screen.getByLabelText('Email'), 'leonid@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'wrong-password')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('invalid email or password')).toBeInTheDocument()
  })

  it('signs in and navigates to the app on success', async () => {
    const fetchMock = mockFetch(() =>
      jsonResponse(200, { user: testUser, csrfToken: 'fresh-csrf' }),
    )
    renderSignIn()

    await userEvent.type(screen.getByLabelText('Email'), 'leonid@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('dashboard home')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/sign-in', expect.anything())
  })
})
