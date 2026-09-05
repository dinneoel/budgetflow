import { z } from 'zod'

const email = z.email('Enter a valid email address')
const password = z.string().min(8, 'Password must be at least 8 characters')
const currency = z
  .string()
  .trim()
  .toUpperCase()
  .regex(/^[A-Z]{3}$/, 'Use a 3-letter currency code (e.g. USD)')

export const signInSchema = z.object({
  email,
  password: z.string().min(1, 'Password is required'),
})

export type SignInValues = z.infer<typeof signInSchema>

export const signUpSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  email,
  password,
  defaultCurrency: currency,
})

export type SignUpValues = z.infer<typeof signUpSchema>

export const resetRequestSchema = z.object({
  email,
})

export type ResetRequestValues = z.infer<typeof resetRequestSchema>

export const resetConfirmSchema = z
  .object({
    token: z.string().trim().min(1, 'Reset code is required'),
    password,
    confirmPassword: z.string(),
  })
  .refine((v) => v.password === v.confirmPassword, {
    message: 'Passwords do not match',
    path: ['confirmPassword'],
  })

export type ResetConfirmValues = z.infer<typeof resetConfirmSchema>

export const profileSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  locale: z.string().trim().min(1, 'Locale is required'),
  timeZone: z.string().trim().min(1, 'Time zone is required'),
  firstDayOfWeek: z.coerce.number<number>().int().min(0).max(6),
  defaultCurrency: currency,
})

export type ProfileValues = z.infer<typeof profileSchema>
