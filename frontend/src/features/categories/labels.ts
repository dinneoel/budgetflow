import type { budgetTypes, rolloverRules } from '../../api/categories'

export const budgetTypeLabels: Record<(typeof budgetTypes)[number], string> = {
  fixed: 'Fixed',
  variable: 'Variable',
  sinking_fund: 'Sinking fund',
  debt: 'Debt',
  savings_goal: 'Savings goal',
}

export const rolloverRuleLabels: Record<(typeof rolloverRules)[number], string> = {
  none: 'No rollover',
  rollover: 'Roll over unused balance',
  reset_to_target: 'Reset to target',
}
