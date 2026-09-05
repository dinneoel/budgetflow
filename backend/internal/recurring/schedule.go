// Package recurring implements recurring bills and subscriptions: CRUD for
// rules, next-due-date computation with month-end clamping, mark-as-paid
// (creates the real transaction and advances the schedule), manual matching
// to an existing transaction, and the upcoming-bills feed that also reserves
// amounts against category budgets.
package recurring

import "time"

// NextDue returns the first scheduled due date strictly after current.
//
// anchor is the schedule's origin (the first due date the user picked) and
// preserves the intended day across month-end clamping: a monthly rule
// anchored on Jan 31 is due Feb 28 (clamped) and then Mar 31 again, and an
// annual rule anchored on Feb 29 falls on Feb 28 in non-leap years.
// customIntervalDays is only used when frequency is "custom".
func NextDue(anchor, current time.Time, frequency string, customIntervalDays int32) time.Time {
	switch frequency {
	case "weekly":
		return current.AddDate(0, 0, 7)
	case "custom":
		return current.AddDate(0, 0, int(customIntervalDays))
	case "annual":
		k := current.Year() - anchor.Year() + 1
		d := addYearsClamped(anchor, k)
		for !d.After(current) {
			k++
			d = addYearsClamped(anchor, k)
		}
		return d
	default: // monthly
		k := (current.Year()-anchor.Year())*12 + int(current.Month()-anchor.Month()) + 1
		d := addMonthsClamped(anchor, k)
		for !d.After(current) {
			k++
			d = addMonthsClamped(anchor, k)
		}
		return d
	}
}

// addMonthsClamped adds months keeping the anchor's day-of-month, clamped to
// the target month's last day (unlike time.AddDate, which normalizes Jan 31 +
// 1 month into Mar 3).
func addMonthsClamped(anchor time.Time, months int) time.Time {
	y := anchor.Year()
	m := int(anchor.Month()) - 1 + months
	y += m / 12
	m = m % 12
	if m < 0 {
		m += 12
		y--
	}
	month := time.Month(m + 1)
	day := min(anchor.Day(), daysIn(y, month))
	return time.Date(y, month, day, 0, 0, 0, 0, anchor.Location())
}

func addYearsClamped(anchor time.Time, years int) time.Time {
	y := anchor.Year() + years
	day := min(anchor.Day(), daysIn(y, anchor.Month()))
	return time.Date(y, anchor.Month(), day, 0, 0, 0, 0, anchor.Location())
}

// daysIn returns the number of days in the given month.
func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
