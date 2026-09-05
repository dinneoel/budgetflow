import { screen, within } from '@testing-library/react'
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

  it('adds a category with icon, color, budget type, and rollover rule', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/categories' && init?.method === 'POST') {
        return jsonResponse(201, { category: cat('c-new', 'Coffee', 2) })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    await userEvent.click(await screen.findByRole('button', { name: '+ Add category' }))
    await userEvent.type(screen.getByLabelText('Category name'), 'Coffee')
    await userEvent.click(screen.getByRole('button', { name: 'Icon 💡' }))
    await userEvent.click(screen.getByRole('button', { name: 'Color Blue' }))
    await userEvent.selectOptions(screen.getByLabelText('Budget type'), 'fixed')
    await userEvent.selectOptions(screen.getByLabelText('Rollover rule'), 'rollover')
    await userEvent.click(screen.getByRole('button', { name: 'Add category' }))

    const call = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/categories' && init?.method === 'POST',
    )
    expect(call).toBeDefined()
    expect(JSON.parse(String(call![1]!.body))).toEqual({
      groupId: group.id,
      name: 'Coffee',
      icon: '💡',
      color: '#3b82f6',
      budgetType: 'fixed',
      rolloverRule: 'rollover',
    })
  })

  it('rejects an empty category name without calling the API', async () => {
    const fetchMock = mockFetch(() => jsonResponse(200, { groups: [group] }))
    renderCategories()

    await userEvent.click(await screen.findByRole('button', { name: '+ Add category' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add category' }))

    expect(await screen.findByText('Category name is required')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([url, init]) => url === '/api/categories' && init?.method === 'POST'),
    ).toBe(false)
  })

  it('edits a category from its row', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/categories/${groceries.id}` && init?.method === 'PUT') {
        return jsonResponse(200, { category: { ...groceries, name: 'Food shop' } })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    const row = (await screen.findByText('Groceries')).closest('li')!
    await userEvent.click(within(row).getByRole('button', { name: 'Edit' }))
    const name = within(row).getByLabelText('Category name')
    expect(name).toHaveValue('Groceries')
    await userEvent.clear(name)
    await userEvent.type(name, 'Food shop')
    await userEvent.click(within(row).getByRole('button', { name: 'Save category' }))

    const call = fetchMock.mock.calls.find(([url]) => url === `/api/categories/${groceries.id}`)
    expect(call).toBeDefined()
    expect(JSON.parse(String(call![1]!.body))).toMatchObject({ name: 'Food shop' })
  })

  it('merges a category into a chosen target', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/categories/${groceries.id}/merge` && init?.method === 'POST') {
        return jsonResponse(200, {
          category: dining,
          transactionsMoved: 3,
          splitsMoved: 0,
          allocationsCombined: 1,
          allocationsReassigned: 1,
        })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    const row = (await screen.findByText('Groceries')).closest('li')!
    const [toggle] = within(row).getAllByRole('button', { name: 'Merge' })
    await userEvent.click(toggle)
    await userEvent.selectOptions(within(row).getByRole('combobox'), dining.id)
    const confirm = within(row)
      .getAllByRole('button', { name: 'Merge' })
      .find((b) => b !== toggle)!
    await userEvent.click(confirm)

    const call = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith(`/categories/${groceries.id}/merge`),
    )
    expect(call).toBeDefined()
    expect(JSON.parse(String(call![1]!.body))).toEqual({ targetId: dining.id })
  })

  it('archives a category from its row', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/categories/${dining.id}/archive` && init?.method === 'POST') {
        return new Response(null, { status: 204 })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    const row = (await screen.findByText('Dining out')).closest('li')!
    await userEvent.click(within(row).getByRole('button', { name: 'Archive' }))

    expect(
      fetchMock.mock.calls.some(([url]) => String(url).endsWith(`/categories/${dining.id}/archive`)),
    ).toBe(true)
  })

  it('renames a group', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === `/api/category-groups/${group.id}` && init?.method === 'PUT') {
        return jsonResponse(200, { group: { ...group, name: 'Essentials' } })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    const card = (await screen.findByRole('region', { name: 'Food' }))
    await userEvent.click(within(card).getByRole('button', { name: 'Rename' }))
    const name = within(card).getByLabelText('Group name')
    await userEvent.clear(name)
    await userEvent.type(name, 'Essentials')
    await userEvent.click(within(card).getByRole('button', { name: 'Save' }))

    const call = fetchMock.mock.calls.find(([url]) => url === `/api/category-groups/${group.id}`)
    expect(call).toBeDefined()
    expect(JSON.parse(String(call![1]!.body))).toEqual({ name: 'Essentials' })
  })

  it('creates a new group', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url === '/api/category-groups' && init?.method === 'POST') {
        return jsonResponse(201, { group: { ...group, id: 'g-new', name: 'Fun', categories: [] } })
      }
      return jsonResponse(200, { groups: [group] })
    })
    renderCategories()

    await userEvent.click(await screen.findByRole('button', { name: 'New group' }))
    await userEvent.type(screen.getByLabelText('New group name'), 'Fun')
    await userEvent.click(screen.getByRole('button', { name: 'Create' }))

    const call = fetchMock.mock.calls.find(([url]) => url === '/api/category-groups')
    expect(call).toBeDefined()
    expect(JSON.parse(String(call![1]!.body))).toEqual({ name: 'Fun' })
  })
})
