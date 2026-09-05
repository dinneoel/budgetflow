import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { SessionInfo } from '../../api/auth'
import type { NotificationPreference } from '../../api/notifications'
import { jsonResponse, mockFetch, renderWithProviders, testUser } from '../../test/utils'
import { SettingsPage } from './SettingsPage'

const preferences: NotificationPreference[] = [
  { type: 'category_threshold', enabled: true, thresholdPct: 80 },
  { type: 'bill_due', enabled: true, thresholdPct: null },
  { type: 'goal_behind_schedule', enabled: false, thresholdPct: null },
]

const sessions: SessionInfo[] = [
  {
    id: 's0000000-0000-4000-8000-000000000001',
    userAgent: 'Firefox on Linux',
    ipAddress: '10.0.0.1',
    createdAt: '2026-09-01T10:00:00Z',
    expiresAt: '2026-10-01T10:00:00Z',
    current: true,
  },
  {
    id: 's0000000-0000-4000-8000-000000000002',
    userAgent: '',
    ipAddress: '10.0.0.2',
    createdAt: '2026-09-02T10:00:00Z',
    expiresAt: '2026-10-02T10:00:00Z',
    current: false,
  },
]

// Serves every read the settings page issues; write endpoints are layered on
// per test.
function defaultHandler(url: string): Response {
  if (url === '/api/auth/me') return jsonResponse(200, { user: testUser, csrfToken: 'tok' })
  if (url === '/api/notifications/preferences') return jsonResponse(200, { preferences })
  if (url === '/api/auth/sessions') return jsonResponse(200, { sessions })
  return jsonResponse(404, { error: `unmocked ${url}` })
}

function renderSettings() {
  return renderWithProviders(
    <Routes>
      <Route path="/settings" element={<SettingsPage />} />
      <Route path="/sign-in" element={<div>sign-in page</div>} />
    </Routes>,
    { route: '/settings' },
  )
}

describe('SettingsPage profile form', () => {
  it('prefills the profile and saves changes', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/profile' && init?.method === 'PUT') {
        return jsonResponse(200, { user: { ...testUser, name: 'Leonid K.' } })
      }
      return defaultHandler(url)
    })
    renderSettings()

    const name = await screen.findByLabelText('Name')
    expect(name).toHaveValue('Leonid')
    expect(screen.getByLabelText('First day of week')).toHaveValue('1')

    await userEvent.clear(name)
    await userEvent.type(name, 'Leonid K.')
    await userEvent.click(screen.getByRole('button', { name: 'Save profile' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Profile saved.')
    const call = fetchMock.mock.calls.find(([url]) => url === '/api/profile')
    expect(JSON.parse(String(call![1]!.body))).toEqual({
      name: 'Leonid K.',
      locale: 'en-US',
      timeZone: 'Europe/Kyiv',
      firstDayOfWeek: 1,
      defaultCurrency: 'UAH',
    })
  })

  it('rejects an empty name without calling the API', async () => {
    const fetchMock = mockFetch((url) => defaultHandler(url))
    renderSettings()

    await userEvent.clear(await screen.findByLabelText('Name'))
    await userEvent.click(screen.getByRole('button', { name: 'Save profile' }))

    expect(await screen.findByText('Name is required')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/profile')).toBe(false)
  })
})

describe('SettingsPage notification preferences', () => {
  it('toggles a preference off', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/notifications/preferences' && init?.method === 'PUT') {
        return jsonResponse(200, { preference: JSON.parse(String(init.body)) })
      }
      return defaultHandler(url)
    })
    renderSettings()

    await userEvent.click(await screen.findByLabelText('Bill due soon'))

    const call = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/notifications/preferences' && init?.method === 'PUT',
    )
    expect(JSON.parse(String(call![1]!.body))).toEqual({
      type: 'bill_due',
      enabled: false,
      thresholdPct: null,
    })
  })

  it('saves a changed warning threshold', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/notifications/preferences' && init?.method === 'PUT') {
        return jsonResponse(200, { preference: JSON.parse(String(init.body)) })
      }
      return defaultHandler(url)
    })
    renderSettings()

    const threshold = await screen.findByLabelText('Warn at')
    expect(threshold).toHaveValue(80)
    await userEvent.clear(threshold)
    await userEvent.type(threshold, '90')
    const section = screen.getByRole('region', { name: 'Notification preferences' })
    await userEvent.click(within(section).getByRole('button', { name: 'Save' }))

    const call = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/notifications/preferences' && init?.method === 'PUT',
    )
    expect(JSON.parse(String(call![1]!.body))).toEqual({
      type: 'category_threshold',
      enabled: true,
      thresholdPct: 90,
    })
  })

  it('disables saving an out-of-range threshold', async () => {
    const fetchMock = mockFetch((url) => defaultHandler(url))
    renderSettings()

    const threshold = await screen.findByLabelText('Warn at')
    await userEvent.clear(threshold)
    await userEvent.type(threshold, '150')

    const section = screen.getByRole('region', { name: 'Notification preferences' })
    expect(within(section).getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(threshold).toHaveAttribute('aria-invalid', 'true')
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) => url === '/api/notifications/preferences' && init?.method === 'PUT',
      ),
    ).toBe(false)
  })
})

describe('SettingsPage security section', () => {
  it('lists sessions and marks the current device', async () => {
    mockFetch((url) => defaultHandler(url))
    renderSettings()

    expect(await screen.findByText('Firefox on Linux')).toBeInTheDocument()
    expect(screen.getByText('This device')).toBeInTheDocument()
    expect(screen.getByText('Unknown device')).toBeInTheDocument()
  })

  it('signs out of all devices and redirects to sign-in', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/auth/sign-out-all' && init?.method === 'POST') {
        return new Response(null, { status: 204 })
      }
      return defaultHandler(url)
    })
    renderSettings()

    await userEvent.click(
      await screen.findByRole('button', { name: 'Sign out of all devices' }),
    )

    expect(await screen.findByText('sign-in page')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/auth/sign-out-all')).toBe(true)
  })
})
