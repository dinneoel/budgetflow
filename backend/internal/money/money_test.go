package money

import (
	"errors"
	"math"
	"testing"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    Money
		want    int64
		wantErr error
	}{
		{"positive", New(100, "USD"), New(250, "USD"), 350, nil},
		{"negative refund", New(100, "USD"), New(-250, "USD"), -150, nil},
		{"both negative", New(-100, "UAH"), New(-250, "UAH"), -350, nil},
		{"zero identity", New(0, "USD"), New(42, "USD"), 42, nil},
		{"currency mismatch", New(100, "USD"), New(100, "EUR"), 0, ErrCurrencyMismatch},
		{"overflow positive", New(math.MaxInt64, "USD"), New(1, "USD"), 0, ErrOverflow},
		{"overflow negative", New(math.MinInt64, "USD"), New(-1, "USD"), 0, ErrOverflow},
		{"max plus min ok", New(math.MaxInt64, "USD"), New(math.MinInt64, "USD"), -1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Add(tt.b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.Amount != tt.want {
				t.Fatalf("amount = %d, want %d", got.Amount, tt.want)
			}
		})
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    Money
		want    int64
		wantErr error
	}{
		{"simple", New(500, "USD"), New(150, "USD"), 350, nil},
		{"goes negative", New(100, "USD"), New(250, "USD"), -150, nil},
		{"subtract negative", New(100, "USD"), New(-50, "USD"), 150, nil},
		{"currency mismatch", New(100, "USD"), New(100, "JPY"), 0, ErrCurrencyMismatch},
		{"overflow", New(math.MinInt64, "USD"), New(1, "USD"), 0, ErrOverflow},
		{"negate min overflow", New(0, "USD"), New(math.MinInt64, "USD"), 0, ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Sub(tt.b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.Amount != tt.want {
				t.Fatalf("amount = %d, want %d", got.Amount, tt.want)
			}
		})
	}
}

func TestNegate(t *testing.T) {
	if got, err := New(-42, "USD").Negate(); err != nil || got.Amount != 42 {
		t.Fatalf("Negate(-42) = %v, %v", got, err)
	}
	if _, err := New(math.MinInt64, "USD").Negate(); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Negate(MinInt64) err = %v, want ErrOverflow", err)
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		n       int
		want    []int64
		wantErr error
	}{
		{"even", 100, 4, []int64{25, 25, 25, 25}, nil},
		{"remainder to leading parts", 100, 3, []int64{34, 33, 33}, nil},
		{"cent across three", 1, 3, []int64{1, 0, 0}, nil},
		{"single part", 77, 1, []int64{77}, nil},
		{"zero amount", 0, 3, []int64{0, 0, 0}, nil},
		{"negative even", -100, 4, []int64{-25, -25, -25, -25}, nil},
		{"negative remainder", -100, 3, []int64{-34, -33, -33}, nil},
		{"more parts than units", 2, 5, []int64{1, 1, 0, 0, 0}, nil},
		{"zero parts", 100, 0, nil, ErrInvalidSplit},
		{"negative parts", 100, -2, nil, ErrInvalidSplit},
		{"min int64 rejected", math.MinInt64, 2, nil, ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := New(tt.amount, "USD").Split(tt.n)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var sum int64
			for i, p := range parts {
				if p.Amount != tt.want[i] {
					t.Fatalf("parts = %v, want %v", parts, tt.want)
				}
				if p.Currency != "USD" {
					t.Fatalf("part %d currency = %q", i, p.Currency)
				}
				sum += p.Amount
			}
			if sum != tt.amount {
				t.Fatalf("sum = %d, want %d (splits must sum exactly to parent)", sum, tt.amount)
			}
		})
	}
}

// TestSplitAlwaysSumsExactly sweeps amounts and part counts to prove the
// invariant beyond hand-picked cases.
func TestSplitAlwaysSumsExactly(t *testing.T) {
	for amount := int64(-1000); amount <= 1000; amount += 7 {
		for n := 1; n <= 13; n++ {
			parts, err := New(amount, "USD").Split(n)
			if err != nil {
				t.Fatalf("Split(%d, %d): %v", amount, n, err)
			}
			var sum int64
			for _, p := range parts {
				sum += p.Amount
			}
			if sum != amount {
				t.Fatalf("Split(%d, %d) sums to %d", amount, n, sum)
			}
		}
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		weights []int64
		want    []int64
		wantErr error
	}{
		{"proportional", 100, []int64{1, 1, 2}, []int64{25, 25, 50}, nil},
		{"remainder by largest fraction", 100, []int64{1, 1, 1}, []int64{34, 33, 33}, nil},
		{"uneven weights", 101, []int64{3, 7}, []int64{30, 71}, nil},
		{"zero weight gets nothing", 100, []int64{0, 1}, []int64{0, 100}, nil},
		{"negative amount", -100, []int64{1, 1, 1}, []int64{-34, -33, -33}, nil},
		{"huge amount no overflow", math.MaxInt64, []int64{1, 1}, []int64{math.MaxInt64/2 + 1, math.MaxInt64 / 2}, nil},
		{"no weights", 100, nil, nil, ErrInvalidSplit},
		{"all zero weights", 100, []int64{0, 0}, nil, ErrInvalidSplit},
		{"negative weight", 100, []int64{1, -1}, nil, ErrInvalidSplit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := New(tt.amount, "USD").Allocate(tt.weights)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var sum int64
			for i, p := range parts {
				if p.Amount != tt.want[i] {
					t.Fatalf("parts = %v, want %v", parts, tt.want)
				}
				sum += p.Amount
			}
			if sum != tt.amount {
				t.Fatalf("sum = %d, want %d", sum, tt.amount)
			}
		})
	}
}

func TestSum(t *testing.T) {
	got, err := Sum([]Money{New(100, "USD"), New(-30, "USD"), New(5, "USD")})
	if err != nil || got.Amount != 75 {
		t.Fatalf("Sum = %v, %v", got, err)
	}
	if _, err := Sum([]Money{New(1, "USD"), New(1, "EUR")}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("mixed-currency Sum err = %v", err)
	}
	if _, err := Sum(nil); err == nil {
		t.Fatal("empty Sum should error")
	}
}

func TestExponent(t *testing.T) {
	tests := []struct {
		currency string
		want     int
	}{
		{"USD", 2}, {"UAH", 2}, {"EUR", 2},
		{"JPY", 0}, {"KRW", 0}, {"ISK", 0},
		{"BHD", 3}, {"KWD", 3},
		{"jpy", 0},  // case-insensitive
		{"XXXX", 2}, // unknown defaults to 2
	}
	for _, tt := range tests {
		if got := Exponent(tt.currency); got != tt.want {
			t.Errorf("Exponent(%q) = %d, want %d", tt.currency, got, tt.want)
		}
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		m    Money
		want string
	}{
		{New(1234, "USD"), "12.34 USD"},
		{New(-1234, "USD"), "-12.34 USD"},
		{New(5, "USD"), "0.05 USD"},
		{New(-5, "UAH"), "-0.05 UAH"},
		{New(0, "USD"), "0.00 USD"},
		{New(500, "JPY"), "500 JPY"},
		{New(-500, "JPY"), "-500 JPY"},
		{New(1234, "BHD"), "1.234 BHD"},
		{New(math.MinInt64, "USD"), "-92233720368547758.08 USD"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("String(%d %s) = %q, want %q", tt.m.Amount, tt.m.Currency, got, tt.want)
		}
	}
}

func TestNewUppercasesCurrency(t *testing.T) {
	if m := New(1, "usd"); m.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", m.Currency)
	}
}
