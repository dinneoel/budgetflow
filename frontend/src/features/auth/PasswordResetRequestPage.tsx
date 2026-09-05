import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { requestPasswordReset } from '../../api/auth'
import { Field, FormError } from '../../components/forms/Field'
import { AuthLayout } from './AuthLayout'
import { resetRequestSchema, type ResetRequestValues } from './schemas'

export function PasswordResetRequestPage() {
  const request = useMutation({
    mutationFn: (values: ResetRequestValues) => requestPasswordReset(values.email),
  })
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ResetRequestValues>({ resolver: zodResolver(resetRequestSchema) })

  return (
    <AuthLayout title="Reset your password">
      {request.isSuccess ? (
        <div className="space-y-4">
          <p role="status" className="text-sm text-gray-700">
            If an account exists for that email, a reset link is on its way. Follow it to choose a
            new password.
          </p>
          <Link to="/sign-in" className="text-sm text-indigo-600 hover:underline">
            Back to sign in
          </Link>
        </div>
      ) : (
        <form onSubmit={handleSubmit((values) => request.mutate(values))} noValidate className="space-y-4">
          <FormError message={request.error?.message} />
          <p className="text-sm text-gray-600">
            Enter your email and we&apos;ll send you a link to reset your password.
          </p>
          <Field
            label="Email"
            type="email"
            autoComplete="email"
            error={errors.email?.message}
            {...register('email')}
          />
          <button
            type="submit"
            disabled={request.isPending}
            className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
          >
            {request.isPending ? 'Sending…' : 'Send reset link'}
          </button>
          <p className="text-center text-sm">
            <Link to="/sign-in" className="text-indigo-600 hover:underline">
              Back to sign in
            </Link>
          </p>
        </form>
      )}
    </AuthLayout>
  )
}
