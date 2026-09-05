import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { confirmPasswordReset } from '../../api/auth'
import { Field, FormError } from '../../components/forms/Field'
import { AuthLayout } from './AuthLayout'
import { resetConfirmSchema, type ResetConfirmValues } from './schemas'

export function PasswordResetConfirmPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const confirm = useMutation({
    mutationFn: (values: ResetConfirmValues) => confirmPasswordReset(values.token, values.password),
    onSuccess: () => navigate('/sign-in', { replace: true }),
  })
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ResetConfirmValues>({
    resolver: zodResolver(resetConfirmSchema),
    defaultValues: { token: searchParams.get('token') ?? '' },
  })

  return (
    <AuthLayout title="Choose a new password">
      <form onSubmit={handleSubmit((values) => confirm.mutate(values))} noValidate className="space-y-4">
        <FormError message={confirm.error?.message} />
        <Field
          label="Reset code"
          type="text"
          error={errors.token?.message}
          {...register('token')}
        />
        <Field
          label="New password"
          type="password"
          autoComplete="new-password"
          error={errors.password?.message}
          {...register('password')}
        />
        <Field
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          error={errors.confirmPassword?.message}
          {...register('confirmPassword')}
        />
        <button
          type="submit"
          disabled={confirm.isPending}
          className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
        >
          {confirm.isPending ? 'Saving…' : 'Set new password'}
        </button>
        <p className="text-center text-sm">
          <Link to="/sign-in" className="text-indigo-600 hover:underline">
            Back to sign in
          </Link>
        </p>
      </form>
    </AuthLayout>
  )
}
