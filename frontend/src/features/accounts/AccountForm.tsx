import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { accountTypes, type AccountInput } from '../../api/accounts'
import { Field, FormError, SelectField } from '../../components/forms/Field'
import { minorToInputString, parseMoneyInput } from '../../lib/money'
import { accountTypeLabels } from './labels'

const accountSchema = z.object({
  name: z.string().trim().min(1, 'Account name is required'),
  institution: z.string().trim(),
  type: z.enum(accountTypes),
  currency: z
    .string()
    .trim()
    .toUpperCase()
    .regex(/^[A-Z]{3}$/, 'Use a 3-letter currency code (e.g. USD)'),
  openingBalance: z
    .string()
    .refine((v) => parseMoneyInput(v) !== null, 'Enter an amount like 1250.00'),
  includeInNetWorth: z.boolean(),
})

type AccountFormValues = z.infer<typeof accountSchema>

interface AccountFormProps {
  defaults?: Partial<AccountInput>
  submitLabel: string
  pending: boolean
  errorMessage?: string
  onSubmit: (input: AccountInput) => void
  onCancel?: () => void
}

// Create/edit form for a manual account; amounts are entered as decimals
// and converted to minor units on submit.
export function AccountForm({
  defaults,
  submitLabel,
  pending,
  errorMessage,
  onSubmit,
  onCancel,
}: AccountFormProps) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<AccountFormValues>({
    resolver: zodResolver(accountSchema),
    defaultValues: {
      name: defaults?.name ?? '',
      institution: defaults?.institution ?? '',
      type: defaults?.type ?? 'checking',
      currency: defaults?.currency ?? '',
      openingBalance:
        defaults?.openingBalance !== undefined ? minorToInputString(defaults.openingBalance) : '0.00',
      includeInNetWorth: defaults?.includeInNetWorth ?? true,
    },
  })

  const submit = handleSubmit((values) =>
    onSubmit({
      name: values.name,
      institution: values.institution,
      type: values.type,
      currency: values.currency,
      openingBalance: parseMoneyInput(values.openingBalance) ?? 0,
      includeInNetWorth: values.includeInNetWorth,
    }),
  )

  return (
    <form onSubmit={submit} noValidate className="space-y-4">
      <FormError message={errorMessage} />
      <Field label="Account name" type="text" error={errors.name?.message} {...register('name')} />
      <Field
        label="Institution"
        type="text"
        placeholder="Optional"
        error={errors.institution?.message}
        {...register('institution')}
      />
      <SelectField label="Type" error={errors.type?.message} {...register('type')}>
        {accountTypes.map((t) => (
          <option key={t} value={t}>
            {accountTypeLabels[t]}
          </option>
        ))}
      </SelectField>
      <Field
        label="Currency"
        type="text"
        maxLength={3}
        placeholder="UAH"
        error={errors.currency?.message}
        {...register('currency')}
      />
      <Field
        label="Opening balance"
        type="text"
        inputMode="decimal"
        error={errors.openingBalance?.message}
        {...register('openingBalance')}
      />
      <label className="flex items-center gap-2 text-sm text-gray-700">
        <input type="checkbox" className="h-4 w-4 rounded" {...register('includeInNetWorth')} />
        Include in net worth
      </label>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
        >
          {pending ? 'Saving…' : submitLabel}
        </button>
        {onCancel ? (
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
          >
            Cancel
          </button>
        ) : null}
      </div>
    </form>
  )
}
