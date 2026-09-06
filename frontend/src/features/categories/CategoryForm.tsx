import { zodResolver } from '@hookform/resolvers/zod'
import { Controller, useForm } from 'react-hook-form'
import { z } from 'zod'
import {
  budgetTypes,
  rolloverRules,
  type CategoryGroup,
  type CategoryInput,
} from '../../api/categories'
import { Field, FormError, SelectField } from '../../components/forms/Field'
import { budgetTypeLabels, rolloverRuleLabels } from './labels'

import { categoryIcon, categoryIcons as icons } from './icons'

const colors = [
  { value: '#ef4444', name: 'Red' },
  { value: '#f97316', name: 'Orange' },
  { value: '#eab308', name: 'Yellow' },
  { value: '#22c55e', name: 'Green' },
  { value: '#06b6d4', name: 'Cyan' },
  { value: '#3b82f6', name: 'Blue' },
  { value: '#8b5cf6', name: 'Violet' },
  { value: '#ec4899', name: 'Pink' },
]

const categorySchema = z.object({
  groupId: z.string().min(1, 'Choose a group'),
  name: z.string().trim().min(1, 'Category name is required'),
  icon: z.string(),
  color: z.string(),
  budgetType: z.enum(budgetTypes),
  rolloverRule: z.enum(rolloverRules),
})

type CategoryFormValues = z.infer<typeof categorySchema>

interface CategoryFormProps {
  groups: CategoryGroup[]
  defaults?: Partial<CategoryInput>
  submitLabel: string
  pending: boolean
  errorMessage?: string
  onSubmit: (input: CategoryInput) => void
  onCancel: () => void
}

export function CategoryForm({
  groups,
  defaults,
  submitLabel,
  pending,
  errorMessage,
  onSubmit,
  onCancel,
}: CategoryFormProps) {
  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<CategoryFormValues>({
    resolver: zodResolver(categorySchema),
    defaultValues: {
      groupId: defaults?.groupId ?? groups[0]?.id ?? '',
      name: defaults?.name ?? '',
      icon: defaults?.icon ?? icons[0],
      color: defaults?.color ?? colors[0].value,
      budgetType: defaults?.budgetType ?? 'variable',
      rolloverRule: defaults?.rolloverRule ?? 'none',
    },
  })

  return (
    <form onSubmit={handleSubmit((v) => onSubmit(v))} noValidate className="space-y-4">
      <FormError message={errorMessage} />
      <Field label="Category name" type="text" error={errors.name?.message} {...register('name')} />
      <SelectField label="Group" error={errors.groupId?.message} {...register('groupId')}>
        {groups
          .filter((g) => !g.archivedAt)
          .map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
      </SelectField>
      <Controller
        control={control}
        name="icon"
        render={({ field }) => (
          <fieldset>
            <legend className="block text-sm font-medium text-gray-700">Icon</legend>
            <div className="mt-1 flex flex-wrap gap-1">
              {icons.map((icon) => (
                <button
                  key={icon}
                  type="button"
                  aria-label={`Icon ${icon}`}
                  aria-pressed={categoryIcon(field.value) === icon}
                  onClick={() => field.onChange(icon)}
                  className={[
                    'flex h-9 w-9 items-center justify-center rounded-md border text-lg',
                    categoryIcon(field.value) === icon
                      ? 'border-indigo-600 bg-indigo-50'
                      : 'border-gray-200 hover:bg-gray-50',
                  ].join(' ')}
                >
                  {icon}
                </button>
              ))}
            </div>
          </fieldset>
        )}
      />
      <Controller
        control={control}
        name="color"
        render={({ field }) => (
          <fieldset>
            <legend className="block text-sm font-medium text-gray-700">Color</legend>
            <div className="mt-1 flex flex-wrap gap-1">
              {colors.map((c) => (
                <button
                  key={c.value}
                  type="button"
                  aria-label={`Color ${c.name}`}
                  aria-pressed={field.value === c.value}
                  onClick={() => field.onChange(c.value)}
                  className={[
                    'h-9 w-9 rounded-md border-2',
                    field.value === c.value ? 'border-gray-900' : 'border-transparent',
                  ].join(' ')}
                  style={{ backgroundColor: c.value }}
                />
              ))}
            </div>
          </fieldset>
        )}
      />
      <SelectField label="Budget type" error={errors.budgetType?.message} {...register('budgetType')}>
        {budgetTypes.map((t) => (
          <option key={t} value={t}>
            {budgetTypeLabels[t]}
          </option>
        ))}
      </SelectField>
      <SelectField
        label="Rollover rule"
        error={errors.rolloverRule?.message}
        {...register('rolloverRule')}
      >
        {rolloverRules.map((r) => (
          <option key={r} value={r}>
            {rolloverRuleLabels[r]}
          </option>
        ))}
      </SelectField>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          {pending ? 'Saving…' : submitLabel}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
        >
          Cancel
        </button>
      </div>
    </form>
  )
}
