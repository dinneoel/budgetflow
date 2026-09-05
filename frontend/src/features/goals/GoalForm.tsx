import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import type { Account } from '../../api/accounts'
import type { CategoryGroup } from '../../api/categories'
import { goalTypeLabels, goalTypes, type Goal, type GoalInput } from '../../api/goals'
import { Field, FormError, SelectField } from '../../components/forms/Field'
import { minorToInputString, parseMoneyInput } from '../../lib/money'

const goalSchema = z.object({
  name: z.string().trim().min(1, 'Goal name is required'),
  type: z.enum(goalTypes),
  targetAmount: z
    .string()
    .refine((v) => (parseMoneyInput(v) ?? 0) > 0, 'Enter a positive amount like 1250.00'),
  targetDate: z
    .string()
    .refine((v) => v === '' || /^\d{4}-\d{2}-\d{2}$/.test(v), 'Enter a date'),
  categoryId: z.string(),
  accountId: z.string(),
})

type GoalFormValues = z.infer<typeof goalSchema>

interface GoalFormProps {
  accounts: Account[]
  groups: CategoryGroup[]
  goal?: Goal
  submitLabel: string
  pending: boolean
  errorMessage?: string
  onSubmit: (input: GoalInput) => void
  onCancel: () => void
}

// Create/edit form for a savings, payoff, or purchase goal.
export function GoalForm({
  accounts,
  groups,
  goal,
  submitLabel,
  pending,
  errorMessage,
  onSubmit,
  onCancel,
}: GoalFormProps) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<GoalFormValues>({
    resolver: zodResolver(goalSchema),
    defaultValues: {
      name: goal?.name ?? '',
      type: goal?.type ?? 'savings',
      targetAmount: goal ? minorToInputString(goal.targetAmount) : '',
      targetDate: goal?.targetDate ?? '',
      categoryId: goal?.categoryId ?? '',
      accountId: goal?.accountId ?? '',
    },
  })

  const submit = handleSubmit((values) =>
    onSubmit({
      name: values.name,
      type: values.type,
      targetAmount: parseMoneyInput(values.targetAmount) ?? 0,
      targetDate: values.targetDate === '' ? null : values.targetDate,
      categoryId: values.categoryId === '' ? null : values.categoryId,
      accountId: values.accountId === '' ? null : values.accountId,
    }),
  )

  return (
    <form onSubmit={submit} noValidate className="space-y-4">
      <FormError message={errorMessage} />
      <Field
        label="Goal name"
        type="text"
        placeholder="Emergency fund"
        error={errors.name?.message}
        {...register('name')}
      />
      <SelectField label="Goal type" error={errors.type?.message} {...register('type')}>
        {goalTypes.map((t) => (
          <option key={t} value={t}>
            {goalTypeLabels[t]}
          </option>
        ))}
      </SelectField>
      <Field
        label="Target amount"
        type="text"
        inputMode="decimal"
        error={errors.targetAmount?.message}
        {...register('targetAmount')}
      />
      <Field
        label="Target date"
        type="date"
        error={errors.targetDate?.message}
        {...register('targetDate')}
      />
      <SelectField
        label="Linked category"
        error={errors.categoryId?.message}
        {...register('categoryId')}
      >
        <option value="">None</option>
        {groups.map((g) => (
          <optgroup key={g.id} label={g.name}>
            {g.categories
              .filter((c) => !c.archivedAt)
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
          </optgroup>
        ))}
      </SelectField>
      <SelectField
        label="Linked account"
        error={errors.accountId?.message}
        {...register('accountId')}
      >
        <option value="">None</option>
        {accounts
          .filter((a) => !a.archivedAt)
          .map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
      </SelectField>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
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
