// Formats an API date string ("YYYY-MM-DD") for display without timezone
// shifts (Date parsing of a bare date is UTC; we render the named parts).
const monthNames = [
  'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
]

export function formatDate(isoDate: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(isoDate)
  if (!m) {
    return isoDate
  }
  const [, year, month, day] = m
  return `${monthNames[Number(month) - 1]} ${Number(day)}, ${year}`
}

// Today's date in the API's YYYY-MM-DD form, in the user's local timezone.
export function todayISO(): string {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}
