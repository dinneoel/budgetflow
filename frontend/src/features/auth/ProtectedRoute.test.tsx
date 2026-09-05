import { screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { ProtectedRoute } from './ProtectedRoute'

function renderProtected(route = '/secret') {
  return renderWithProviders(
    <Routes>
      <Route path="/sign-in" element={<div>sign-in page</div>} />
      <Route element={<ProtectedRoute />}>
        <Route path="/secret" element={<div>secret content</div>} />
      </Route>
    </Routes>,
    { route },
  )
}

describe('ProtectedRoute', () => {
  it('redirects to sign-in when the session check returns 401', async () => {
    mockFetch(() => jsonResponse(401, { error: 'unauthenticated' }))
    renderProtected()

    expect(await screen.findByText('sign-in page')).toBeInTheDocument()
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
  })

  it('renders the protected content for an authenticated user', async () => {
    mockFetch(() => jsonResponse(200, { user: testUser, csrfToken: 'csrf' }))
    renderProtected()

    expect(await screen.findByText('secret content')).toBeInTheDocument()
  })

  it('shows a loading state while the session check is pending', () => {
    mockFetch(() => new Promise<Response>(() => {}))
    renderProtected()

    expect(screen.getByRole('status')).toHaveTextContent('Loading…')
  })
})
