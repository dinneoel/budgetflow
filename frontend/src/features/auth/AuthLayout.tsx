import type { ReactNode } from 'react'

// Centered card wrapper shared by the public auth pages.
export function AuthLayout({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="flex min-h-screen items-center justify-center bg-gray-50 px-4 py-12">
      <div className="w-full max-w-md">
        <p className="text-center text-2xl font-bold text-indigo-600">BudgetFlow</p>
        <h1 className="mt-2 text-center text-xl font-semibold text-gray-900">{title}</h1>
        <div className="mt-6 rounded-lg bg-white p-6 shadow">{children}</div>
      </div>
    </main>
  )
}
