import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { Field, FormError } from '../../components/forms/Field'
import { AuthLayout } from './AuthLayout'
import { signUpSchema, type SignUpValues } from './schemas'
import { useSignUp } from './useAuth'

export function SignUpPage() {
  const navigate = useNavigate()
  const signUp = useSignUp()
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<SignUpValues>({
    resolver: zodResolver(signUpSchema),
    defaultValues: { defaultCurrency: 'USD' },
  })

  const onSubmit = handleSubmit((values) => {
    signUp.mutate(values, {
      onSuccess: () => navigate('/', { replace: true }),
    })
  })

  return (
    <AuthLayout title="Create your account">
      <form onSubmit={onSubmit} noValidate className="space-y-4">
        <FormError message={signUp.error?.message} />
        <Field
          label="Name"
          type="text"
          autoComplete="name"
          error={errors.name?.message}
          {...register('name')}
        />
        <Field
          label="Email"
          type="email"
          autoComplete="email"
          error={errors.email?.message}
          {...register('email')}
        />
        <Field
          label="Password"
          type="password"
          autoComplete="new-password"
          error={errors.password?.message}
          {...register('password')}
        />
        <Field
          label="Default currency"
          type="text"
          maxLength={3}
          error={errors.defaultCurrency?.message}
          {...register('defaultCurrency')}
        />
        <button
          type="submit"
          disabled={signUp.isPending}
          className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
        >
          {signUp.isPending ? 'Creating account…' : 'Create account'}
        </button>
      </form>
      <p className="mt-4 text-center text-sm text-gray-600">
        Already have an account?{' '}
        <Link to="/sign-in" className="text-indigo-600 hover:underline">
          Sign in
        </Link>
      </p>
    </AuthLayout>
  )
}
