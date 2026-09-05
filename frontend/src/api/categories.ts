import { del, get, post, put } from './client'

export const budgetTypes = ['fixed', 'variable', 'sinking_fund', 'debt', 'savings_goal'] as const
export type BudgetType = (typeof budgetTypes)[number]

export const rolloverRules = ['none', 'rollover', 'reset_to_target'] as const
export type RolloverRule = (typeof rolloverRules)[number]

export interface Category {
  id: string
  groupId: string
  name: string
  icon: string
  color: string
  budgetType: BudgetType
  rolloverRule: RolloverRule
  sortOrder: number
  archivedAt: string | null
  createdAt: string
  updatedAt: string
}

export interface CategoryGroup {
  id: string
  name: string
  sortOrder: number
  archivedAt: string | null
  createdAt: string
  updatedAt: string
  categories: Category[]
}

export interface CategoryInput {
  groupId: string
  name: string
  icon: string
  color: string
  budgetType: BudgetType
  rolloverRule: RolloverRule
}

export interface MergeResult {
  category: Category
  transactionsMoved: number
  splitsMoved: number
  allocationsCombined: number
  allocationsReassigned: number
}

export async function listCategoryGroups(signal?: AbortSignal): Promise<CategoryGroup[]> {
  const res = await get<{ groups: CategoryGroup[] }>('/categories', signal)
  // The API omits the categories key for groups with none.
  return res.groups.map((g) => ({ ...g, categories: g.categories ?? [] }))
}

export async function createGroup(name: string): Promise<void> {
  await post('/category-groups', { name })
}

export async function updateGroup(id: string, name: string): Promise<void> {
  await put(`/category-groups/${id}`, { name })
}

export async function deleteGroup(id: string): Promise<void> {
  await del(`/category-groups/${id}`)
}

export async function archiveGroup(id: string, archived: boolean): Promise<void> {
  await post(`/category-groups/${id}/${archived ? 'archive' : 'unarchive'}`)
}

export async function reorderGroups(ids: string[]): Promise<void> {
  await put('/category-groups/reorder', { ids })
}

export async function createCategory(input: CategoryInput): Promise<void> {
  await post('/categories', input)
}

export async function updateCategory(id: string, input: CategoryInput): Promise<void> {
  await put(`/categories/${id}`, input)
}

export async function deleteCategory(id: string): Promise<void> {
  await del(`/categories/${id}`)
}

export async function archiveCategory(id: string, archived: boolean): Promise<void> {
  await post(`/categories/${id}/${archived ? 'archive' : 'unarchive'}`)
}

export async function reorderCategories(ids: string[]): Promise<void> {
  await put('/categories/reorder', { ids })
}

export async function mergeCategory(sourceId: string, targetId: string): Promise<MergeResult> {
  return post<MergeResult>(`/categories/${sourceId}/merge`, { targetId })
}

export async function seedDefaultCategories(): Promise<CategoryGroup[]> {
  const res = await post<{ groups: CategoryGroup[] }>('/categories/seed-defaults')
  return res.groups
}
