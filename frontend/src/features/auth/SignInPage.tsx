import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { Field, FormError } from '../../components/forms/Field'
import { AuthLayout } from './AuthLayout'
import { signInSchema, type SignInValues } from './schemas'
import { useSignIn } from './useAuth'

export function SignInPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const signIn = useSignIn()
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<SignInValues>({ resolver: zodResolver(signInSchema) })

  const from = (location.state as { from?: { pathname: string } } | null)?.from?.pathname ?? '/'

  const onSubmit = handleSubmit((values) => {
    signIn.mutate(values, {
      onSuccess: () => navigate(from, { replace: true }),
    })
  })

  return (
    <AuthLayout title="Sign in to your account">
      <form onSubmit={onSubmit} noValidate className="space-y-4">
        <FormError message={signIn.error?.message} />
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
          autoComplete="current-password"
          error={errors.password?.message}
          {...register('password')}
        />
        <button
          type="submit"
          disabled={signIn.isPending}
          className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
        >
          {signIn.isPending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <div className="mt-4 flex justify-between text-sm">
        <Link to="/password-reset" className="text-indigo-600 hover:underline">
          Forgot password?
        </Link>
        <Link to="/sign-up" className="text-indigo-600 hover:underline">
          Create an account
        </Link>
      </div>
    </AuthLayout>
  )
}
