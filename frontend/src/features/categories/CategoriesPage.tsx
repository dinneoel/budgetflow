import { useState } from 'react'
import type { Category, CategoryGroup, CategoryInput } from '../../api/categories'
import { EmptyState } from '../../components/EmptyState'
import { CategoryForm } from './CategoryForm'
import { budgetTypeLabels, rolloverRuleLabels } from './labels'
import {
  useArchiveCategory,
  useArchiveGroup,
  useCategoryGroups,
  useCreateCategory,
  useCreateGroup,
  useMergeCategory,
  useReorderCategories,
  useReorderGroups,
  useSeedDefaults,
  useUpdateCategory,
  useUpdateGroup,
} from './useCategories'

// Moves the item at index i one position in dir and returns the new id order.
function movedIds(items: { id: string }[], i: number, dir: -1 | 1): string[] {
  const ids = items.map((x) => x.id)
  const j = i + dir
  ;[ids[i], ids[j]] = [ids[j], ids[i]]
  return ids
}

function MergeDialog({
  source,
  groups,
  onClose,
}: {
  source: Category
  groups: CategoryGroup[]
  onClose: () => void
}) {
  const merge = useMergeCategory()
  const [targetId, setTargetId] = useState('')
  const candidates = groups.flatMap((g) =>
    g.categories.filter((c) => c.id !== source.id && !c.archivedAt).map((c) => ({ ...c, groupName: g.name })),
  )

  return (
    <div className="mt-2 rounded-md border border-gray-200 bg-gray-50 p-3">
      <label htmlFor={`merge-target-${source.id}`} className="block text-sm font-medium text-gray-700">
        Merge “{source.name}” into
      </label>
      <p className="mt-1 text-xs text-gray-600">
        Transactions and allocations move to the target; this category is archived.
      </p>
      <select
        id={`merge-target-${source.id}`}
        value={targetId}
        onChange={(e) => setTargetId(e.target.value)}
        className="mt-2 block w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm"
      >
        <option value="">Choose a category…</option>
        {candidates.map((c) => (
          <option key={c.id} value={c.id}>
            {c.groupName} · {c.name}
          </option>
        ))}
      </select>
      {merge.error ? (
        <p role="alert" className="mt-1 text-sm text-red-600">
          {merge.error.message}
        </p>
      ) : null}
      <div className="mt-2 flex gap-2">
        <button
          type="button"
          disabled={!targetId || merge.isPending}
          onClick={() =>
            merge.mutate({ sourceId: source.id, targetId }, { onSuccess: onClose })
          }
          className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          Merge
        </button>
        <button
          type="button"
          onClick={onClose}
          className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}

function CategoryRow({
  category,
  group,
  groups,
  index,
}: {
  category: Category
  group: CategoryGroup
  groups: CategoryGroup[]
  index: number
}) {
  const update = useUpdateCategory()
  const archive = useArchiveCategory()
  const reorder = useReorderCategories()
  const [editing, setEditing] = useState(false)
  const [merging, setMerging] = useState(false)
  const archived = Boolean(category.archivedAt)

  return (
    <li className="border-t border-gray-100 py-2 first:border-t-0">
      <div className="flex items-center gap-2">
        <span
          aria-hidden="true"
          className="flex h-8 w-8 items-center justify-center rounded-md text-base"
          style={{ backgroundColor: category.color || '#e5e7eb' }}
        >
          {category.icon}
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium text-gray-900">
            {category.name}
            {archived ? <span className="ml-2 text-xs font-normal text-gray-500">Archived</span> : null}
          </p>
          <p className="text-xs text-gray-600">
            {budgetTypeLabels[category.budgetType]} · {rolloverRuleLabels[category.rolloverRule]}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button
            type="button"
            aria-label={`Move ${category.name} up`}
            disabled={index === 0 || reorder.isPending}
            onClick={() => reorder.mutate(movedIds(group.categories, index, -1))}
            className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100 disabled:opacity-40"
          >
            ↑
          </button>
          <button
            type="button"
            aria-label={`Move ${category.name} down`}
            disabled={index === group.categories.length - 1 || reorder.isPending}
            onClick={() => reorder.mutate(movedIds(group.categories, index, 1))}
            className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100 disabled:opacity-40"
          >
            ↓
          </button>
          <button
            type="button"
            onClick={() => setEditing((v) => !v)}
            className="rounded-md px-2 py-1 text-sm text-indigo-600 hover:bg-indigo-50"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => setMerging((v) => !v)}
            className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
          >
            Merge
          </button>
          <button
            type="button"
            onClick={() => archive.mutate({ id: category.id, archived: !archived })}
            className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
          >
            {archived ? 'Unarchive' : 'Archive'}
          </button>
        </div>
      </div>
      {editing ? (
        <div className="mt-2 rounded-md border border-gray-200 bg-gray-50 p-3">
          <CategoryForm
            groups={groups}
            defaults={category}
            submitLabel="Save category"
            pending={update.isPending}
            errorMessage={update.error?.message}
            onCancel={() => setEditing(false)}
            onSubmit={(input: CategoryInput) =>
              update.mutate({ id: category.id, input }, { onSuccess: () => setEditing(false) })
            }
          />
        </div>
      ) : null}
      {merging ? (
        <MergeDialog source={category} groups={groups} onClose={() => setMerging(false)} />
      ) : null}
    </li>
  )
}

function GroupCard({
  group,
  groups,
  index,
}: {
  group: CategoryGroup
  groups: CategoryGroup[]
  index: number
}) {
  const updateGroup = useUpdateGroup()
  const archiveGroup = useArchiveGroup()
  const reorderGroups = useReorderGroups()
  const create = useCreateCategory()
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(group.name)
  const [adding, setAdding] = useState(false)
  const archived = Boolean(group.archivedAt)

  return (
    <section aria-label={group.name} className="rounded-lg bg-white p-4 shadow">
      <div className="flex items-center gap-2">
        {renaming ? (
          <form
            className="flex flex-1 gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              updateGroup.mutate({ id: group.id, name }, { onSuccess: () => setRenaming(false) })
            }}
          >
            <label htmlFor={`group-name-${group.id}`} className="sr-only">
              Group name
            </label>
            <input
              id={`group-name-${group.id}`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="block flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm"
            />
            <button
              type="submit"
              disabled={updateGroup.isPending}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              Save
            </button>
            <button
              type="button"
              onClick={() => {
                setName(group.name)
                setRenaming(false)
              }}
              className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700"
            >
              Cancel
            </button>
          </form>
        ) : (
          <h2 className="flex-1 text-lg font-semibold text-gray-900">
            {group.name}
            {archived ? <span className="ml-2 text-xs font-normal text-gray-500">Archived</span> : null}
          </h2>
        )}
        {!renaming ? (
          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              aria-label={`Move group ${group.name} up`}
              disabled={index === 0 || reorderGroups.isPending}
              onClick={() => reorderGroups.mutate(movedIds(groups, index, -1))}
              className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100 disabled:opacity-40"
            >
              ↑
            </button>
            <button
              type="button"
              aria-label={`Move group ${group.name} down`}
              disabled={index === groups.length - 1 || reorderGroups.isPending}
              onClick={() => reorderGroups.mutate(movedIds(groups, index, 1))}
              className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100 disabled:opacity-40"
            >
              ↓
            </button>
            <button
              type="button"
              onClick={() => setRenaming(true)}
              className="rounded-md px-2 py-1 text-sm text-indigo-600 hover:bg-indigo-50"
            >
              Rename
            </button>
            <button
              type="button"
              onClick={() => archiveGroup.mutate({ id: group.id, archived: !archived })}
              className="rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
            >
              {archived ? 'Unarchive' : 'Archive'}
            </button>
          </div>
        ) : null}
      </div>

      <ul className="mt-3">
        {group.categories.map((c, i) => (
          <CategoryRow key={c.id} category={c} group={group} groups={groups} index={i} />
        ))}
        {group.categories.length === 0 ? (
          <li className="py-2 text-sm text-gray-500">No categories in this group yet.</li>
        ) : null}
      </ul>

      {adding ? (
        <div className="mt-2 rounded-md border border-gray-200 bg-gray-50 p-3">
          <CategoryForm
            groups={groups}
            defaults={{ groupId: group.id }}
            submitLabel="Add category"
            pending={create.isPending}
            errorMessage={create.error?.message}
            onCancel={() => setAdding(false)}
            onSubmit={(input) => create.mutate(input, { onSuccess: () => setAdding(false) })}
          />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mt-2 rounded-md px-2 py-1 text-sm font-medium text-indigo-600 hover:bg-indigo-50"
        >
          + Add category
        </button>
      )}
    </section>
  )
}

export function CategoriesPage() {
  const { data: groups, isLoading } = useCategoryGroups()
  const createGroup = useCreateGroup()
  const seed = useSeedDefaults()
  const [addingGroup, setAddingGroup] = useState(false)
  const [groupName, setGroupName] = useState('')

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Categories</h1>
        <button
          type="button"
          onClick={() => setAddingGroup(true)}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
        >
          New group
        </button>
      </div>

      {addingGroup ? (
        <form
          className="mt-4 flex max-w-md gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            createGroup.mutate(groupName, {
              onSuccess: () => {
                setGroupName('')
                setAddingGroup(false)
              },
            })
          }}
        >
          <label htmlFor="new-group-name" className="sr-only">
            New group name
          </label>
          <input
            id="new-group-name"
            value={groupName}
            onChange={(e) => setGroupName(e.target.value)}
            placeholder="Group name"
            className="block flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm"
          />
          <button
            type="submit"
            disabled={createGroup.isPending || !groupName.trim()}
            className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            Create
          </button>
          <button
            type="button"
            onClick={() => setAddingGroup(false)}
            className="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700"
          >
            Cancel
          </button>
        </form>
      ) : null}
      {createGroup.error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {createGroup.error.message}
        </p>
      ) : null}

      {!isLoading && groups && groups.length === 0 ? (
        <EmptyState
          title="No categories yet"
          description="Start from the recommended set of groups and categories, or build your own from scratch."
        >
          <button
            type="button"
            disabled={seed.isPending}
            onClick={() => seed.mutate()}
            className="mt-4 inline-block rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            {seed.isPending ? 'Adding…' : 'Use recommended categories'}
          </button>
          {seed.error ? (
            <p role="alert" className="mt-2 text-sm text-red-600">
              {seed.error.message}
            </p>
          ) : null}
        </EmptyState>
      ) : null}

      <div className="mt-6 max-w-3xl space-y-4">
        {(groups ?? []).map((g, i) => (
          <GroupCard key={g.id} group={g} groups={groups ?? []} index={i} />
        ))}
      </div>
    </>
  )
}
