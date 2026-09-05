import type { accountTypes } from '../../api/accounts'

export const accountTypeLabels: Record<(typeof accountTypes)[number], string> = {
  cash: 'Cash',
  checking: 'Checking',
  savings: 'Savings',
  credit_card: 'Credit card',
  ewallet: 'E-wallet',
  custom: 'Custom',
}
