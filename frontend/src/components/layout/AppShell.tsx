import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useIsDesktop } from '../../hooks/useMediaQuery'
import { useCurrentUser, useSignOut } from '../../features/auth/useAuth'

const navItems = [
  { to: '/', label: 'Dashboard' },
  { to: '/budget', label: 'Budget' },
  { to: '/transactions', label: 'Transactions' },
  { to: '/goals', label: 'Goals' },
  { to: '/reports', label: 'Reports' },
]

function navLinkClass({ isActive }: { isActive: boolean }) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium',
    isActive ? 'bg-indigo-100 text-indigo-700' : 'text-gray-700 hover:bg-gray-100',
  ].join(' ')
}

function QuickAddButton({ className }: { className?: string }) {
  return (
    <Link
      to="/transactions?quick-add=1"
      aria-label="Add transaction"
      className={[
        'inline-flex items-center justify-center rounded-full bg-indigo-600 text-white shadow-lg hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2',
        className ?? '',
      ].join(' ')}
    >
      <span aria-hidden="true" className="text-2xl leading-none">
        +
      </span>
    </Link>
  )
}

function DesktopSidebar() {
  const { data: user } = useCurrentUser()
  const signOut = useSignOut()
  const navigate = useNavigate()

  return (
    <aside className="flex w-60 shrink-0 flex-col border-r border-gray-200 bg-white px-4 py-6">
      <p className="px-3 text-xl font-bold text-indigo-600">BudgetFlow</p>
      <nav aria-label="Primary" className="mt-6 flex flex-1 flex-col gap-1">
        {navItems.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.to === '/'} className={navLinkClass}>
            {item.label}
          </NavLink>
        ))}
      </nav>
      <div className="mt-6 space-y-1 border-t border-gray-200 pt-4">
        <QuickAddButton className="mb-3 h-10 w-full rounded-md" />
        <NavLink to="/settings" className={navLinkClass}>
          Settings
        </NavLink>
        <button
          type="button"
          onClick={() => signOut.mutate(undefined, { onSettled: () => navigate('/sign-in') })}
          className="block w-full rounded-md px-3 py-2 text-left text-sm font-medium text-gray-700 hover:bg-gray-100"
        >
          Sign out{user ? ` (${user.name || user.email})` : ''}
        </button>
      </div>
    </aside>
  )
}

function MobileNav() {
  return (
    <>
      <QuickAddButton className="fixed right-4 bottom-20 z-20 h-14 w-14" />
      <nav
        aria-label="Primary"
        className="fixed inset-x-0 bottom-0 z-10 flex items-stretch justify-around border-t border-gray-200 bg-white"
      >
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              [
                'flex-1 px-1 py-3 text-center text-xs font-medium',
                isActive ? 'text-indigo-700' : 'text-gray-600',
              ].join(' ')
            }
          >
            {item.label}
          </NavLink>
        ))}
      </nav>
    </>
  )
}

function MobileHeader() {
  return (
    <header className="flex items-center justify-between border-b border-gray-200 bg-white px-4 py-3">
      <p className="text-lg font-bold text-indigo-600">BudgetFlow</p>
      <Link to="/settings" className="text-sm font-medium text-gray-700">
        Settings
      </Link>
    </header>
  )
}

// Application chrome for authenticated screens: persistent sidebar on
// desktop, bottom navigation plus floating quick-add on mobile.
export function AppShell() {
  const isDesktop = useIsDesktop()

  return (
    <div className="flex min-h-screen bg-gray-50">
      {isDesktop ? <DesktopSidebar /> : null}
      <div className="flex min-w-0 flex-1 flex-col">
        {!isDesktop ? <MobileHeader /> : null}
        <main className={['flex-1 p-4 md:p-8', isDesktop ? '' : 'pb-24'].join(' ')}>
          <Outlet />
        </main>
      </div>
      {!isDesktop ? <MobileNav /> : null}
    </div>
  )
}
