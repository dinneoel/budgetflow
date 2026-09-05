import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { AppNotification } from '../../api/notifications'
import { jsonResponse, mockFetch, renderWithProviders } from '../../test/utils'
import { NotificationCenter } from './NotificationCenter'

const overBudget: AppNotification = {
  id: 'n0000000-0000-4000-8000-000000000001',
  type: 'category_over_budget',
  title: 'Groceries is over budget',
  body: 'Spending in Groceries exceeds its available funds by UAH 120.00. Move money from another category to cover it.',
  actionUrl: '/budget',
  read: false,
  readAt: null,
  createdAt: '2026-09-05T10:00:00Z',
}

const billDue: AppNotification = {
  id: 'n0000000-0000-4000-8000-000000000002',
  type: 'bill_due',
  title: 'Rent is due tomorrow',
  body: 'Mark it paid once the payment goes through, or match it to an existing transaction.',
  actionUrl: '/recurring',
  read: true,
  readAt: '2026-09-04T10:00:00Z',
  createdAt: '2026-09-04T09:00:00Z',
}

function handler(notifications: AppNotification[]) {
  return (url: string, init?: RequestInit) => {
    const unread = notifications.filter((n) => !n.read).length
    if (url === '/api/notifications/unread-count') {
      return jsonResponse(200, { unreadCount: unread })
    }
    if (url === '/api/notifications/evaluate' && init?.method === 'POST') {
      return jsonResponse(200, { unreadCount: unread })
    }
    if (url.startsWith('/api/notifications?')) {
      return jsonResponse(200, { notifications, unreadCount: unread, limit: 50, offset: 0 })
    }
    if (url.endsWith('/read') && init?.method === 'POST') {
      const id = url.split('/')[3]
      const n = notifications.find((x) => x.id === id)
      if (n) {
        n.read = true
      }
      return new Response(null, { status: 204 })
    }
    if (url === '/api/notifications/read-all' && init?.method === 'POST') {
      notifications.forEach((n) => {
        n.read = true
      })
      return new Response(null, { status: 204 })
    }
    return jsonResponse(404, { error: `unhandled ${url}` })
  }
}

describe('NotificationCenter', () => {
  it('shows the unread count on the bell', async () => {
    mockFetch(handler([{ ...overBudget }, { ...billDue }]))
    renderWithProviders(<NotificationCenter />)

    expect(
      await screen.findByRole('button', { name: 'Notifications (1 unread)' }),
    ).toBeInTheDocument()
  })

  it('opens the panel, runs the evaluator, and distinguishes read from unread', async () => {
    const fetchMock = mockFetch(handler([{ ...overBudget }, { ...billDue }]))
    renderWithProviders(<NotificationCenter />)

    await userEvent.click(await screen.findByRole('button', { name: /Notifications/ }))

    expect(await screen.findByText('Groceries is over budget')).toBeInTheDocument()
    expect(screen.getByText('Rent is due tomorrow')).toBeInTheDocument()
    // The suggested action from the server copy is shown.
    expect(screen.getByText(/Move money from another category/)).toBeInTheDocument()
    // Only the unread item offers mark-read; the read one does not.
    expect(screen.getAllByRole('button', { name: 'Mark read' })).toHaveLength(1)
    expect(screen.getByText('(unread)')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) => url === '/api/notifications/evaluate' && init?.method === 'POST',
      ),
    ).toBe(true)
    // Items link to the screen the warning is about.
    expect(screen.getByRole('link', { name: /Groceries is over budget/ })).toHaveAttribute(
      'href',
      '/budget',
    )
  })

  it('marks a single notification read and clears its badge', async () => {
    const data = [{ ...overBudget }, { ...billDue }]
    const fetchMock = mockFetch(handler(data))
    renderWithProviders(<NotificationCenter />)

    await userEvent.click(await screen.findByRole('button', { name: /Notifications/ }))
    await userEvent.click(await screen.findByRole('button', { name: 'Mark read' }))

    expect(
      fetchMock.mock.calls.some(
        ([url, init]) => url === `/api/notifications/${overBudget.id}/read` && init?.method === 'POST',
      ),
    ).toBe(true)
    // After the refetch there is nothing unread left to mark.
    expect(await screen.findByRole('button', { name: 'Notifications' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark read' })).not.toBeInTheDocument()
  })

  it('marks all notifications read', async () => {
    const data = [{ ...overBudget }, { ...billDue, read: false, readAt: null }]
    const fetchMock = mockFetch(handler(data))
    renderWithProviders(<NotificationCenter />)

    await userEvent.click(await screen.findByRole('button', { name: /Notifications/ }))
    await userEvent.click(await screen.findByRole('button', { name: 'Mark all read' }))

    expect(
      fetchMock.mock.calls.some(
        ([url, init]) => url === '/api/notifications/read-all' && init?.method === 'POST',
      ),
    ).toBe(true)
    expect(await screen.findByRole('button', { name: 'Notifications' })).toBeInTheDocument()
  })
})
