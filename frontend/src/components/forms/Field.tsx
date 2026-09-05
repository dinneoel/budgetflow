import { useId, type ComponentProps } from 'react'

interface FieldProps extends ComponentProps<'input'> {
  label: string
  error?: string
}

// Labeled input with accessible error wiring (aria-invalid + aria-describedby).
export function Field({ label, error, id, className, ...inputProps }: FieldProps) {
  const generatedId = useId()
  const inputId = id ?? generatedId
  const errorId = `${inputId}-error`
  return (
    <div className={className}>
      <label htmlFor={inputId} className="block text-sm font-medium text-gray-700">
        {label}
      </label>
      <input
        id={inputId}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500 focus:outline-none focus-visible:ring-2"
        {...inputProps}
      />
      {error ? (
        <p id={errorId} role="alert" className="mt-1 text-sm text-red-600">
          {error}
        </p>
      ) : null}
    </div>
  )
}

interface SelectFieldProps extends ComponentProps<'select'> {
  label: string
  error?: string
}

export function SelectField({
  label,
  error,
  id,
  className,
  children,
  ...selectProps
}: SelectFieldProps) {
  const generatedId = useId()
  const selectId = id ?? generatedId
  const errorId = `${selectId}-error`
  return (
    <div className={className}>
      <label htmlFor={selectId} className="block text-sm font-medium text-gray-700">
        {label}
      </label>
      <select
        id={selectId}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        className="mt-1 block w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500 focus:outline-none focus-visible:ring-2"
        {...selectProps}
      >
        {children}
      </select>
      {error ? (
        <p id={errorId} role="alert" className="mt-1 text-sm text-red-600">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function FormError({ message }: { message: string | undefined }) {
  if (!message) return null
  return (
    <div role="alert" className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
      {message}
    </div>
  )
}
