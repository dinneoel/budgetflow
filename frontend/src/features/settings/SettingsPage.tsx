import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router-dom'
import { Field, FormError, SelectField } from '../../components/forms/Field'
import { profileSchema, type ProfileValues } from '../auth/schemas'
import { useCurrentUser, useUpdateProfile } from '../auth/useAuth'
import { DataSection } from './DataSection'
import { NotificationPreferencesSection } from './NotificationPreferencesSection'
import { SecuritySection } from './SecuritySection'

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

function ProfileForm({ defaults }: { defaults: ProfileValues }) {
  const update = useUpdateProfile()
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ProfileValues>({
    resolver: zodResolver(profileSchema),
    defaultValues: defaults,
  })

  return (
    <form
      onSubmit={handleSubmit((values) => update.mutate(values))}
      noValidate
      className="mt-6 max-w-lg space-y-4 rounded-lg bg-white p-6 shadow"
    >
      <FormError message={update.error?.message} />
      {update.isSuccess ? (
        <p role="status" className="rounded-md bg-green-50 px-3 py-2 text-sm text-green-700">
          Profile saved.
        </p>
      ) : null}
      <Field label="Name" type="text" error={errors.name?.message} {...register('name')} />
      <Field
        label="Locale"
        type="text"
        placeholder="en-US"
        error={errors.locale?.message}
        {...register('locale')}
      />
      <Field
        label="Time zone"
        type="text"
        placeholder="Europe/Kyiv"
        error={errors.timeZone?.message}
        {...register('timeZone')}
      />
      <SelectField
        label="First day of week"
        error={errors.firstDayOfWeek?.message}
        {...register('firstDayOfWeek')}
      >
        {weekdays.map((day, i) => (
          <option key={day} value={i}>
            {day}
          </option>
        ))}
      </SelectField>
      <Field
        label="Default currency"
        type="text"
        maxLength={3}
        error={errors.defaultCurrency?.message}
        {...register('defaultCurrency')}
      />
      <button
        type="submit"
        disabled={update.isPending}
        className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50"
      >
        {update.isPending ? 'Saving…' : 'Save profile'}
      </button>
    </form>
  )
}

export function SettingsPage() {
  const { data: user } = useCurrentUser()

  return (
    <>
      <h1 className="text-2xl font-bold text-gray-900">Settings</h1>

      <nav aria-label="Manage" className="mt-4 flex max-w-lg gap-2">
        <Link
          to="/accounts"
          className="flex-1 rounded-lg bg-white px-4 py-3 text-sm font-medium text-gray-900 shadow hover:bg-gray-50"
        >
          Accounts
        </Link>
        <Link
          to="/categories"
          className="flex-1 rounded-lg bg-white px-4 py-3 text-sm font-medium text-gray-900 shadow hover:bg-gray-50"
        >
          Categories
        </Link>
      </nav>

      <h2 className="mt-6 text-lg font-semibold text-gray-900">Profile</h2>
      {user ? (
        <ProfileForm
          defaults={{
            name: user.name,
            locale: user.locale,
            timeZone: user.timeZone,
            firstDayOfWeek: user.firstDayOfWeek,
            defaultCurrency: user.defaultCurrency,
          }}
        />
      ) : null}

      <NotificationPreferencesSection />
      <SecuritySection />
      <DataSection />
    </>
  )
}
