package recurring

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestNextDue(t *testing.T) {
	tests := []struct {
		name      string
		anchor    time.Time
		current   time.Time
		frequency string
		interval  int32
		want      time.Time
	}{
		{"weekly", date(2026, 9, 4), date(2026, 9, 4), "weekly", 0, date(2026, 9, 11)},
		{"weekly across month end", date(2026, 9, 25), date(2026, 9, 25), "weekly", 0, date(2026, 10, 2)},
		{"custom 10 days", date(2026, 9, 1), date(2026, 9, 1), "custom", 10, date(2026, 9, 11)},
		{"custom across year end", date(2026, 12, 28), date(2026, 12, 28), "custom", 7, date(2027, 1, 4)},

		{"monthly mid-month", date(2026, 9, 15), date(2026, 9, 15), "monthly", 0, date(2026, 10, 15)},
		{"monthly 31st clamps to Feb 28", date(2026, 1, 31), date(2026, 1, 31), "monthly", 0, date(2026, 2, 28)},
		{"monthly recovers 31st after clamp", date(2026, 1, 31), date(2026, 2, 28), "monthly", 0, date(2026, 3, 31)},
		{"monthly 31st clamps to Apr 30", date(2026, 1, 31), date(2026, 3, 31), "monthly", 0, date(2026, 4, 30)},
		{"monthly recovers 31st after Apr", date(2026, 1, 31), date(2026, 4, 30), "monthly", 0, date(2026, 5, 31)},
		{"monthly 31st clamps to Feb 29 in leap year", date(2028, 1, 31), date(2028, 1, 31), "monthly", 0, date(2028, 2, 29)},
		{"monthly 30th clamps to Feb only", date(2026, 1, 30), date(2026, 1, 30), "monthly", 0, date(2026, 2, 28)},
		{"monthly across year end", date(2026, 12, 31), date(2026, 12, 31), "monthly", 0, date(2027, 1, 31)},

		{"annual", date(2026, 6, 15), date(2026, 6, 15), "annual", 0, date(2027, 6, 15)},
		{"annual Feb 29 clamps to Feb 28", date(2024, 2, 29), date(2024, 2, 29), "annual", 0, date(2025, 2, 28)},
		{"annual keeps Feb 29 anchor through common years", date(2024, 2, 29), date(2025, 2, 28), "annual", 0, date(2026, 2, 28)},
		{"annual recovers Feb 29 in next leap year", date(2024, 2, 29), date(2027, 2, 28), "annual", 0, date(2028, 2, 29)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextDue(tt.anchor, tt.current, tt.frequency, tt.interval)
			if !got.Equal(tt.want) {
				t.Errorf("NextDue(%s, %s, %s, %d) = %s, want %s",
					tt.anchor.Format("2006-01-02"), tt.current.Format("2006-01-02"),
					tt.frequency, tt.interval, got.Format("2006-01-02"), tt.want.Format("2006-01-02"))
			}
		})
	}
}

func TestNextDueSequence(t *testing.T) {
	// A rule anchored on Jan 31 must clamp in short months and return to the
	// 31st in long ones as it is advanced occurrence by occurrence.
	anchor := date(2026, 1, 31)
	want := []time.Time{
		date(2026, 2, 28), date(2026, 3, 31), date(2026, 4, 30), date(2026, 5, 31),
		date(2026, 6, 30), date(2026, 7, 31), date(2026, 8, 31), date(2026, 9, 30),
		date(2026, 10, 31), date(2026, 11, 30), date(2026, 12, 31), date(2027, 1, 31),
	}
	current := anchor
	for i, w := range want {
		current = NextDue(anchor, current, "monthly", 0)
		if !current.Equal(w) {
			t.Fatalf("occurrence %d = %s, want %s", i+1, current.Format("2006-01-02"), w.Format("2006-01-02"))
		}
	}
}
