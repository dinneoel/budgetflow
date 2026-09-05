package budgetmath

import (
	"math"
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestCategoryRemaining(t *testing.T) {
	tests := []struct {
		name string
		c    Category
		want int64
	}{
		{"simple spend", Category{Budgeted: 10000, Spending: 4000}, 6000},
		{"with rollover", Category{Budgeted: 10000, Rollover: 2500, Spending: 4000}, 8500},
		{"with reserved upcoming", Category{Budgeted: 10000, Spending: 4000, Reserved: 5000}, 1000},
		{"reserved pushes over", Category{Budgeted: 10000, Spending: 8000, Reserved: 5000}, -3000},
		{"net refund increases remaining", Category{Budgeted: 10000, Spending: -1500}, 11500},
		{"zero budget with spending", Category{Spending: 2000}, -2000},
		{"all zero", Category{}, 0},
		{"negative rollover from adjustment", Category{Budgeted: 5000, Rollover: -1000, Spending: 1000}, 3000},
		{"overspent", Category{Budgeted: 3000, Spending: 4500}, -1500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Remaining(); got != tt.want {
				t.Fatalf("Remaining() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUnallocated(t *testing.T) {
	tests := []struct {
		name        string
		income      int64
		rollover    int64
		allocations []int64
		want        int64
	}{
		{"fully allocated", 100000, 0, []int64{60000, 40000}, 0},
		{"under-allocated", 100000, 0, []int64{60000}, 40000},
		{"over-allocated is negative", 100000, 0, []int64{60000, 50000}, -10000},
		{"prior rollover adds funds", 100000, 5000, []int64{100000}, 5000},
		{"no allocations", 100000, 0, nil, 100000},
		{"zero income", 0, 0, []int64{1000}, -1000},
		{"negative prior rollover", 100000, -2000, []int64{98000}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Unallocated(tt.income, tt.rollover, tt.allocations...); got != tt.want {
				t.Fatalf("Unallocated() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		c         Category
		threshold int
		want      Status
	}{
		{"plenty left", Category{Budgeted: 10000, Spending: 2000}, 20, StatusOnTrack},
		{"exactly at threshold", Category{Budgeted: 10000, Spending: 8000}, 20, StatusApproachingLimit},
		{"just above threshold", Category{Budgeted: 10000, Spending: 7999}, 20, StatusOnTrack},
		{"fully spent", Category{Budgeted: 10000, Spending: 10000}, 20, StatusApproachingLimit},
		{"one unit over", Category{Budgeted: 10000, Spending: 10001}, 20, StatusOverBudget},
		{"reserved counts toward limit", Category{Budgeted: 10000, Spending: 5000, Reserved: 4000}, 20, StatusApproachingLimit},
		{"reserved pushes over", Category{Budgeted: 10000, Spending: 7000, Reserved: 4000}, 20, StatusOverBudget},
		{"rollover extends funds", Category{Budgeted: 10000, Rollover: 5000, Spending: 11000}, 20, StatusOnTrack},
		{"spending with no funds", Category{Spending: 100}, 20, StatusUnfunded},
		{"reserved with no funds", Category{Reserved: 100}, 20, StatusUnfunded},
		{"no funds no activity", Category{}, 20, StatusOnTrack},
		{"refund with no funds", Category{Spending: -500}, 20, StatusOnTrack},
		{"net refund on funded", Category{Budgeted: 1000, Spending: -200}, 20, StatusOnTrack},
		{"zero threshold only flags fully spent", Category{Budgeted: 10000, Spending: 9999}, 0, StatusOnTrack},
		{"zero threshold fully spent", Category{Budgeted: 10000, Spending: 10000}, 0, StatusApproachingLimit},
		{"100 threshold always approaching", Category{Budgeted: 10000, Spending: 1}, 100, StatusApproachingLimit},
		{"100 threshold untouched", Category{Budgeted: 10000}, 100, StatusApproachingLimit},
		{"negative available overspent is unfunded", Category{Budgeted: 1000, Rollover: -2000, Spending: 500}, 20, StatusUnfunded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Classify(tt.threshold); got != tt.want {
				t.Fatalf("Classify(%d) = %q, want %q", tt.threshold, got, tt.want)
			}
		})
	}
}

func TestNextRollover(t *testing.T) {
	tests := []struct {
		name      string
		rule      RolloverRule
		remaining int64
		target    int64
		want      int64
	}{
		{"none discards surplus", RolloverNone, 5000, 0, 0},
		{"none discards deficit", RolloverNone, -5000, 0, 0},
		{"rollover carries surplus", RolloverUnused, 5000, 0, 5000},
		{"rollover never carries deficit", RolloverUnused, -5000, 0, 0},
		{"rollover zero", RolloverUnused, 0, 0, 0},
		{"reset caps at target", RolloverResetToTarget, 8000, 5000, 5000},
		{"reset under target carries all", RolloverResetToTarget, 3000, 5000, 3000},
		{"reset exact target", RolloverResetToTarget, 5000, 5000, 5000},
		{"reset never negative", RolloverResetToTarget, -1000, 5000, 0},
		{"reset with zero target", RolloverResetToTarget, 3000, 0, 0},
		{"reset with negative target", RolloverResetToTarget, 3000, -100, 0},
		{"unknown rule carries nothing", RolloverRule("bogus"), 5000, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRollover(tt.rule, tt.remaining, tt.target); got != tt.want {
				t.Fatalf("NextRollover(%q, %d, %d) = %d, want %d", tt.rule, tt.remaining, tt.target, got, tt.want)
			}
		})
	}
}

// TestRolloverAcrossPeriods chains two periods end to end: October's leftover
// funds November per each rule.
func TestRolloverAcrossPeriods(t *testing.T) {
	october := Category{Budgeted: 10000, Spending: 6000}
	remaining := october.Remaining() // 4000 left in October

	for _, tt := range []struct {
		rule RolloverRule
		want int64 // November remaining after budgeting 10000, spending 2000
	}{
		{RolloverNone, 8000},           // 10000 - 2000
		{RolloverUnused, 12000},        // 10000 + 4000 - 2000
		{RolloverResetToTarget, 11000}, // carried capped at target 3000
	} {
		november := Category{
			Budgeted: 10000,
			Rollover: NextRollover(tt.rule, remaining, 3000),
			Spending: 2000,
		}
		if got := november.Remaining(); got != tt.want {
			t.Errorf("rule %q: November remaining = %d, want %d", tt.rule, got, tt.want)
		}
	}
}

func TestMonthsRemaining(t *testing.T) {
	tests := []struct {
		name   string
		now    time.Time
		target time.Time
		want   int
	}{
		{"later this month", date(2026, 9, 5), date(2026, 9, 30), 1},
		{"next month", date(2026, 9, 5), date(2026, 10, 1), 2},
		{"end of year", date(2026, 9, 5), date(2026, 12, 31), 4},
		{"across year boundary", date(2026, 11, 15), date(2027, 2, 1), 4},
		{"same day", date(2026, 9, 5), date(2026, 9, 5), 1},
		{"in the past", date(2026, 9, 5), date(2026, 8, 31), 0},
		{"a year out", date(2026, 9, 5), date(2027, 9, 5), 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MonthsRemaining(tt.now, tt.target); got != tt.want {
				t.Fatalf("MonthsRemaining = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRequiredMonthlyContribution(t *testing.T) {
	tests := []struct {
		name   string
		goal   Goal
		months int
		want   int64
	}{
		{"even division", Goal{Target: 120000, Current: 0}, 12, 10000},
		{"rounds up", Goal{Target: 100000, Current: 0}, 3, 33334},
		{"partial progress", Goal{Target: 100000, Current: 40000}, 6, 10000},
		{"met target", Goal{Target: 100000, Current: 100000}, 6, 0},
		{"exceeded target", Goal{Target: 100000, Current: 150000}, 6, 0},
		{"no months left", Goal{Target: 100000, Current: 30000}, 0, 70000},
		{"negative months treated as due", Goal{Target: 100000, Current: 30000}, -2, 70000},
		{"one month", Goal{Target: 99999, Current: 0}, 1, 99999},
		{"tiny remainder many months", Goal{Target: 5, Current: 0}, 12, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.goal.RequiredMonthlyContribution(tt.months); got != tt.want {
				t.Fatalf("RequiredMonthlyContribution(%d) = %d, want %d", tt.months, got, tt.want)
			}
		})
	}
}

func TestBehindSchedule(t *testing.T) {
	start := date(2026, 1, 1)
	target := date(2027, 1, 1)  // 365 days
	halfway := date(2026, 7, 2) // just past the midpoint

	tests := []struct {
		name string
		goal Goal
		now  time.Time
		want bool
	}{
		{"on pace at midpoint", Goal{Target: 100000, Current: 50000}, halfway, false},
		{"behind at midpoint", Goal{Target: 100000, Current: 30000}, halfway, true},
		{"ahead at midpoint", Goal{Target: 100000, Current: 80000}, halfway, false},
		{"met target never behind", Goal{Target: 100000, Current: 100000}, date(2027, 6, 1), false},
		{"exceeded target never behind", Goal{Target: 100000, Current: 120000}, halfway, false},
		{"past due unmet always behind", Goal{Target: 100000, Current: 99999}, date(2027, 1, 1), true},
		{"long past due", Goal{Target: 100000, Current: 0}, date(2028, 1, 1), true},
		{"before start nothing expected", Goal{Target: 100000, Current: 0}, date(2025, 12, 1), false},
		{"at start nothing expected", Goal{Target: 100000, Current: 0}, start, false},
		{"just after start tiny expectation", Goal{Target: 100000, Current: 0}, date(2026, 1, 2), true},
		{"huge goal no overflow", Goal{Target: math.MaxInt64, Current: math.MaxInt64 / 4}, halfway, true},
		{"huge goal on pace", Goal{Target: math.MaxInt64 - 1, Current: math.MaxInt64/2 + 100}, halfway, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.goal.BehindSchedule(start, tt.now, target); got != tt.want {
				t.Fatalf("BehindSchedule(now=%s) = %v, want %v", tt.now.Format("2006-01-02"), got, tt.want)
			}
		})
	}

	t.Run("degenerate target not after start", func(t *testing.T) {
		g := Goal{Target: 100, Current: 0}
		if g.BehindSchedule(start, date(2025, 6, 1), start) {
			t.Fatal("now before target date == start should not be behind")
		}
	})
}

func TestDefaultWarningThreshold(t *testing.T) {
	if DefaultWarningThresholdPct != 20 {
		t.Fatalf("default threshold = %d, want 20 per PRD", DefaultWarningThresholdPct)
	}
}
