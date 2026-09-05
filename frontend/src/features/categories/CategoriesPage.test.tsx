import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Category, CategoryGroup } from '../../api/categories'
import { jsonResponse, mockFetch, renderWithProviders } from '../../test/utils'
import { CategoriesPage } from './CategoriesPage'

function cat(id: string, name: string, sortOrder: number): Category {
  return {
    id,
    groupId: 'g0000000-0000-4000-8000-000000000001',
    name,
    icon: '🍎',
    color: '#22c55e',
    budgetType: 'variable',
    rolloverRule: 'none',
    sortOrder,
    archivedAt: null,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z',
  }
}

const groceries = cat('c0000000-0000-4000-8000-000000000001', 'Groceries', 0)
const dining = cat('c0000000-0000-4000-8000-000000000002', 'Dining out', 1)

const group: CategoryGroup = {
  id: 'g0000000-0000-4000-8000-000000000001',
  name: 'Food',
  sortOrder: 0,
  archivedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  categories: [groceries, dining],
}

function renderCategories() {
  return renderWithProviders(
    <Routes>
      <Route path="/categories" element={<CategoriesPage />} />
    </Routes>,
    { route: '/categories' },
  )
}

describe('CategoriesPage', () => {
  it('reorders categories within a group', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/categories/reorder' && init?.method === 'PUT') {
        return new Response(null, { status: 204 })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    await userEvent.click(await screen.findByRole('button', { name: 'Move Groceries down' }))

    const reorderCall = fetchMock.mock.calls.find(([url]) => url === '/api/categories/reorder')
    expect(reorderCall).toBeDefined()
    expect(JSON.parse(String(reorderCall![1]!.body))).toEqual({
      ids: [dining.id, groceries.id],
    })
  })

  it('disables moving the first category up and the last down', async () => {
    mockFetch(() => jsonResponse(200, { groups: [group] }))
    renderCategories()

    expect(await screen.findByRole('button', { name: 'Move Groceries up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move Dining out down' })).toBeDisabled()
  })

  it('offers the recommended set when there are no categories', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/categories/seed-defaults' && init?.method === 'POST') {
        return jsonResponse(201, { groups: [group] })
      }
      return jsonResponse(200, { groups: [] })
    })
    renderCategories()

    await userEvent.click(
      await screen.findByRole('button', { name: 'Use recommended categories' }),
    )

    const seedCall = fetchMock.mock.calls.find(([url]) => url === '/api/categories/seed-defaults')
    expect(seedCall).toBeDefined()
  })
})
