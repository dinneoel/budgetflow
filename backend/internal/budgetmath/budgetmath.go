// Package budgetmath is the pure calculation engine for BudgetFlow's
// zero-based budgeting model. All amounts are int64 minor units in the budget
// period's single currency (enforced upstream); this package never touches
// the database or does currency conversion.
package budgetmath

import (
	"math/big"
	"time"
)

// DefaultWarningThresholdPct is the "approaching limit" warning threshold:
// a category is flagged when its remaining balance falls to this percentage
// of its available funds or below.
const DefaultWarningThresholdPct = 20

// Category holds the inputs for one category in one budget period.
type Category struct {
	Budgeted int64 // allocated this period
	Rollover int64 // carried in from the prior period
	Spending int64 // actual activity: positive = net spending, negative = net refund
	Reserved int64 // upcoming recurring payments reserved against this category
}

// Available is the total funding of the category this period.
func (c Category) Available() int64 { return c.Budgeted + c.Rollover }

// Remaining implements the PRD formula:
// Remaining = Budgeted + Rollover − ActualSpending − ReservedUpcomingPayments.
func (c Category) Remaining() int64 {
	return c.Budgeted + c.Rollover - c.Spending - c.Reserved
}

// Unallocated implements the period-level zero-based formula:
// Unallocated = AvailableIncome + PriorRollover − Σ Allocations.
// A negative result means the user has over-allocated.
func Unallocated(availableIncome, priorRollover int64, allocations ...int64) int64 {
	u := availableIncome + priorRollover
	for _, a := range allocations {
		u -= a
	}
	return u
}

// Status classifies a category's health for badges and notifications.
type Status string

const (
	StatusOnTrack          Status = "on_track"
	StatusApproachingLimit Status = "approaching_limit"
	StatusOverBudget       Status = "over_budget"
	StatusUnfunded         Status = "unfunded"
)

// Classify returns the category status using the given warning threshold
// percentage (use DefaultWarningThresholdPct for the PRD default of 20%):
//   - unfunded: there is spending or reservations but no funds at all
//   - over_budget: remaining is negative
//   - approaching_limit: remaining is within thresholdPct% of available funds
//     (a fully spent category, remaining == 0, is approaching, not over)
//   - on_track: everything else, including zero-activity and net-refund
//     categories
func (c Category) Classify(thresholdPct int) Status {
	remaining := c.Remaining()
	available := c.Available()
	switch {
	case available <= 0 && remaining < 0:
		return StatusUnfunded
	case remaining < 0:
		return StatusOverBudget
	case available > 0 && remaining*100 <= available*int64(thresholdPct):
		return StatusApproachingLimit
	default:
		return StatusOnTrack
	}
}

// RolloverRule mirrors the categories.rollover_rule schema values.
type RolloverRule string

const (
	RolloverNone          RolloverRule = "none"            // unused funds return to the pool
	RolloverUnused        RolloverRule = "rollover"        // unused balance carries forward
	RolloverResetToTarget RolloverRule = "reset_to_target" // carry unused balance, capped at target
)

// NextRollover computes the amount carried into the next period when it is
// created. Overspending never carries as negative rollover — a shortfall is
// surfaced as over_budget in the closing period instead. target is only used
// by reset_to_target (a sinking fund's cap); pass 0 otherwise.
func NextRollover(rule RolloverRule, remaining, target int64) int64 {
	carried := max(remaining, 0)
	switch rule {
	case RolloverUnused:
		return carried
	case RolloverResetToTarget:
		return min(carried, max(target, 0))
	default: // RolloverNone and any unknown rule carry nothing
		return 0
	}
}

// Goal holds the inputs for savings/payoff/purchase goal math.
type Goal struct {
	Target  int64 // target amount, > 0
	Current int64 // sum of contributions so far
}

// AmountRemaining is how much is still needed; never negative.
func (g Goal) AmountRemaining() int64 {
	return max(g.Target-g.Current, 0)
}

// MonthsRemaining counts contribution opportunities: the number of distinct
// calendar months from now through the target date's month, inclusive. A
// target later this month is 1; a target in the past is 0.
func MonthsRemaining(now, targetDate time.Time) int {
	if targetDate.Before(now) {
		return 0
	}
	months := (targetDate.Year()-now.Year())*12 + int(targetDate.Month()-now.Month())
	return months + 1
}

// RequiredMonthlyContribution is the level monthly amount that reaches the
// target on time, rounded up so the goal is met rather than missed by
// rounding. With no months left, the whole remainder is due now.
func (g Goal) RequiredMonthlyContribution(monthsRemaining int) int64 {
	remaining := g.AmountRemaining()
	if remaining == 0 {
		return 0
	}
	if monthsRemaining <= 0 {
		return remaining
	}
	m := int64(monthsRemaining)
	return (remaining + m - 1) / m
}

// BehindSchedule reports whether the goal is behind a linear pace from start
// to targetDate. A met goal is never behind; an unmet goal past its target
// date always is. Before the start date nothing is expected yet.
func (g Goal) BehindSchedule(start, now, targetDate time.Time) bool {
	if g.Current >= g.Target {
		return false
	}
	if !now.Before(targetDate) {
		return true
	}
	if !now.After(start) || !targetDate.After(start) {
		return false
	}
	// expected = Target * elapsed / total, in big.Int because
	// target × seconds overflows int64 for large goals.
	elapsed := big.NewInt(int64(now.Sub(start)))
	total := big.NewInt(int64(targetDate.Sub(start)))
	expected := new(big.Int).Div(new(big.Int).Mul(big.NewInt(g.Target), elapsed), total)
	return big.NewInt(g.Current).Cmp(expected) < 0
}
