import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'
import type { Account } from '../../api/accounts'
import type { CategoryGroup } from '../../api/categories'
import { frequencies, frequencyLabels, type RecurringRule, type RecurringRuleInput } from '../../api/recurring'
import { Field, FormError, SelectField } from '../../components/forms/Field'
import { minorToInputString, parseMoneyInput } from '../../lib/money'

const ruleSchema = z
  .object({
    name: z.string().trim().min(1, 'Name is required'),
    accountId: z.string().min(1, 'Choose an account'),
    categoryId: z.string().min(1, 'Choose a category'),
    amount: z
      .string()
      .refine((v) => (parseMoneyInput(v) ?? 0) > 0, 'Enter a positive amount like 1250.00'),
    frequency: z.enum(frequencies),
    customIntervalDays: z.string(),
    nextDueDate: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'Choose the next due date'),
    reminderLeadDays: z
      .string()
      .regex(/^\d+$/, 'Enter the number of days before the due date to remind you'),
  })
  .refine(
    (v) => v.frequency !== 'custom' || /^[1-9]\d*$/.test(v.customIntervalDays),
    { path: ['customIntervalDays'], message: 'Enter the interval in days' },
  )

type RuleFormValues = z.infer<typeof ruleSchema>

interface RecurringRuleFormProps {
  accounts: Account[]
  groups: CategoryGroup[]
  rule?: RecurringRule
  submitLabel: string
  pending: boolean
  errorMessage?: string
  onSubmit: (input: RecurringRuleInput) => void
  onCancel: () => void
}

// Create/edit form for a recurring bill or subscription.
export function RecurringRuleForm({
  accounts,
  groups,
  rule,
  submitLabel,
  pending,
  errorMessage,
  onSubmit,
  onCancel,
}: RecurringRuleFormProps) {
  const {
    register,
    handleSubmit,
    control,
    formState: { errors },
  } = useForm<RuleFormValues>({
    resolver: zodResolver(ruleSchema),
    defaultValues: {
      name: rule?.name ?? '',
      accountId: rule?.accountId ?? '',
      categoryId: rule?.categoryId ?? '',
      amount: rule ? minorToInputString(rule.amount) : '',
      frequency: rule?.frequency ?? 'monthly',
      customIntervalDays: rule?.customIntervalDays ? String(rule.customIntervalDays) : '',
      nextDueDate: rule?.nextDueDate ?? '',
      reminderLeadDays: String(rule?.reminderLeadDays ?? 3),
    },
  })

  const frequency = useWatch({ control, name: 'frequency' })

  const submit = handleSubmit((values) =>
    onSubmit({
      name: values.name,
      accountId: values.accountId,
      categoryId: values.categoryId,
      amount: parseMoneyInput(values.amount) ?? 0,
      frequency: values.frequency,
      customIntervalDays:
        values.frequency === 'custom' ? Number(values.customIntervalDays) : null,
      nextDueDate: values.nextDueDate,
      reminderLeadDays: Number(values.reminderLeadDays),
    }),
  )

  return (
    <form onSubmit={submit} noValidate className="space-y-4">
      <FormError message={errorMessage} />
      <Field label="Name" type="text" placeholder="Rent" error={errors.name?.message} {...register('name')} />
      <SelectField label="Account" error={errors.accountId?.message} {...register('accountId')}>
        <option value="">Choose an account…</option>
        {accounts
          .filter((a) => !a.archivedAt)
          .map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
      </SelectField>
      <SelectField label="Category" error={errors.categoryId?.message} {...register('categoryId')}>
        <option value="">Choose a category…</option>
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
      <Field
        label="Amount"
        type="text"
        inputMode="decimal"
        error={errors.amount?.message}
        {...register('amount')}
      />
      <SelectField label="Frequency" error={errors.frequency?.message} {...register('frequency')}>
        {frequencies.map((f) => (
          <option key={f} value={f}>
            {frequencyLabels[f]}
          </option>
        ))}
      </SelectField>
      {frequency === 'custom' ? (
        <Field
          label="Interval (days)"
          type="number"
          min={1}
          error={errors.customIntervalDays?.message}
          {...register('customIntervalDays')}
        />
      ) : null}
      <Field
        label="Next due date"
        type="date"
        error={errors.nextDueDate?.message}
        {...register('nextDueDate')}
      />
      <Field
        label="Remind me (days before)"
        type="number"
        min={0}
        error={errors.reminderLeadDays?.message}
        {...register('reminderLeadDays')}
      />
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
