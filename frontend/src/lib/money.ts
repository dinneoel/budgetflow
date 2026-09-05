// Money helpers: the API exchanges amounts as int64 minor units; the UI
// shows and accepts decimal strings in the account's currency.

export function formatMoney(minorUnits: number, currency: string, locale = 'en-US'): string {
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(minorUnits / 100)
  } catch {
    // Unknown currency code — fall back to a plain decimal with the code.
    return `${(minorUnits / 100).toFixed(2)} ${currency}`
  }
}

// Parses user input like "1,234.56", "-12", "0.5" into minor units.
// Returns null for anything that is not a plain decimal amount.
export function parseMoneyInput(input: string): number | null {
  const cleaned = input.trim().replace(/,/g, '')
  if (!/^-?\d+(\.\d{1,2})?$/.test(cleaned)) {
    return null
  }
  const negative = cleaned.startsWith('-')
  const [whole, frac = ''] = (negative ? cleaned.slice(1) : cleaned).split('.')
  const minor = Number(whole) * 100 + Number(frac.padEnd(2, '0'))
  return negative ? -minor : minor
}

// Renders minor units as a plain decimal string for form defaults ("12.50").
export function minorToInputString(minorUnits: number): string {
  const negative = minorUnits < 0
  const abs = Math.abs(minorUnits)
  const whole = Math.floor(abs / 100)
  const frac = String(abs % 100).padStart(2, '0')
  return `${negative ? '-' : ''}${whole}.${frac}`
}
