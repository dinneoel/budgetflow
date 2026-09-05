// Package money implements an exact integer money type. Amounts are int64
// minor units (cents, kopiykas) — never floats — tagged with an ISO 4217
// currency code. All arithmetic is overflow-checked and mixing currencies is
// an error, never a silent conversion.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

var (
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	ErrOverflow         = errors.New("money: int64 overflow")
	ErrInvalidSplit     = errors.New("money: invalid split")
)

// Money is an amount in minor units of a single currency.
type Money struct {
	Amount   int64
	Currency string
}

// New returns a Money value. The currency code is uppercased but not
// validated against a registry: unknown codes format with 2 decimal places.
func New(amount int64, currency string) Money {
	return Money{Amount: amount, Currency: strings.ToUpper(currency)}
}

// Zero returns the zero amount in the given currency.
func Zero(currency string) Money { return New(0, currency) }

func (m Money) IsZero() bool     { return m.Amount == 0 }
func (m Money) IsNegative() bool { return m.Amount < 0 }
func (m Money) IsPositive() bool { return m.Amount > 0 }

// SameCurrency reports whether both values share one currency.
func (m Money) SameCurrency(o Money) bool { return m.Currency == o.Currency }

// Add returns m + o, failing on currency mismatch or int64 overflow.
func (m Money) Add(o Money) (Money, error) {
	if !m.SameCurrency(o) {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, o.Currency)
	}
	sum := m.Amount + o.Amount
	// Overflow iff both operands share a sign and the result does not.
	if (m.Amount > 0 && o.Amount > 0 && sum < 0) || (m.Amount < 0 && o.Amount < 0 && sum >= 0) {
		return Money{}, ErrOverflow
	}
	return Money{Amount: sum, Currency: m.Currency}, nil
}

// Sub returns m - o, failing on currency mismatch or int64 overflow.
func (m Money) Sub(o Money) (Money, error) {
	neg, err := o.Negate()
	if err != nil {
		return Money{}, err
	}
	if !m.SameCurrency(o) {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, o.Currency)
	}
	return m.Add(neg)
}

// Negate returns -m. It fails only for math.MinInt64, which has no positive
// counterpart.
func (m Money) Negate() (Money, error) {
	if m.Amount == math.MinInt64 {
		return Money{}, ErrOverflow
	}
	return Money{Amount: -m.Amount, Currency: m.Currency}, nil
}

// Split divides m into n parts that sum exactly to m. The remainder is
// distributed one minor unit at a time to the leading parts, so parts differ
// by at most one unit. Works for negative amounts (parts are all <= 0).
func (m Money) Split(n int) ([]Money, error) {
	if n < 1 {
		return nil, fmt.Errorf("%w: %d parts", ErrInvalidSplit, n)
	}
	if m.Amount == math.MinInt64 {
		// |MinInt64| is not representable, so remainder distribution below
		// cannot use absolute values; reject this single unusable amount.
		return nil, ErrOverflow
	}
	base := m.Amount / int64(n)
	rem := m.Amount % int64(n)
	sign := int64(1)
	if rem < 0 {
		sign, rem = -1, -rem
	}
	parts := make([]Money, n)
	for i := range parts {
		amt := base
		if int64(i) < rem {
			amt += sign
		}
		parts[i] = Money{Amount: amt, Currency: m.Currency}
	}
	return parts, nil
}

// Allocate divides m proportionally to the given non-negative weights,
// summing exactly to m. Leftover minor units go to the parts with the
// largest fractional remainders (ties broken by lower index). At least one
// weight must be positive.
func (m Money) Allocate(weights []int64) ([]Money, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("%w: no weights", ErrInvalidSplit)
	}
	total := big.NewInt(0)
	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("%w: negative weight %d", ErrInvalidSplit, w)
		}
		total.Add(total, big.NewInt(w))
	}
	if total.Sign() == 0 {
		return nil, fmt.Errorf("%w: all weights zero", ErrInvalidSplit)
	}

	amount := big.NewInt(m.Amount)
	parts := make([]Money, len(weights))
	remainders := make([]*big.Int, len(weights))
	distributed := big.NewInt(0)
	for i, w := range weights {
		// Floor division toward zero, remainder carries the fraction.
		q, r := new(big.Int).QuoRem(new(big.Int).Mul(amount, big.NewInt(w)), total, new(big.Int))
		parts[i] = Money{Amount: q.Int64(), Currency: m.Currency}
		r.Abs(r)
		remainders[i] = r
		distributed.Add(distributed, q)
	}
	// Leftover is at most len(weights)-1 units; hand them out by largest
	// remainder so the allocation is as proportional as possible.
	leftover := new(big.Int).Sub(amount, distributed).Int64()
	sign := int64(1)
	if leftover < 0 {
		sign, leftover = -1, -leftover
	}
	for ; leftover > 0; leftover-- {
		best := -1
		for i, r := range remainders {
			if r.Sign() > 0 && (best == -1 || r.Cmp(remainders[best]) > 0) {
				best = i
			}
		}
		if best == -1 {
			return nil, ErrInvalidSplit // unreachable: leftover implies remainders
		}
		parts[best].Amount += sign
		remainders[best].SetInt64(0)
	}
	return parts, nil
}

// Sum adds all values, which must share one currency. Sum of an empty slice
// is an error because the currency would be unknown.
func Sum(values []Money) (Money, error) {
	if len(values) == 0 {
		return Money{}, fmt.Errorf("%w: empty sum", ErrCurrencyMismatch)
	}
	acc := values[0]
	var err error
	for _, v := range values[1:] {
		if acc, err = acc.Add(v); err != nil {
			return Money{}, err
		}
	}
	return acc, nil
}

// zeroDecimal and threeDecimal list ISO 4217 currencies whose minor unit is
// not the default 10^-2.
var zeroDecimal = map[string]bool{
	"BIF": true, "CLP": true, "DJF": true, "GNF": true, "ISK": true,
	"JPY": true, "KMF": true, "KRW": true, "PYG": true, "RWF": true,
	"UGX": true, "UYI": true, "VND": true, "VUV": true, "XAF": true,
	"XOF": true, "XPF": true,
}

var threeDecimal = map[string]bool{
	"BHD": true, "IQD": true, "JOD": true, "KWD": true, "LYD": true,
	"OMR": true, "TND": true,
}

// Exponent returns the number of decimal places of the currency's minor
// unit (2 for USD/EUR/UAH, 0 for JPY, 3 for BHD). Unknown codes default to 2.
func Exponent(currency string) int {
	c := strings.ToUpper(currency)
	switch {
	case zeroDecimal[c]:
		return 0
	case threeDecimal[c]:
		return 3
	default:
		return 2
	}
}

// String formats the amount in major units with the currency's exponent,
// e.g. -1234 USD → "-12.34 USD", 500 JPY → "500 JPY".
func (m Money) String() string {
	exp := Exponent(m.Currency)
	if exp == 0 {
		return fmt.Sprintf("%d %s", m.Amount, m.Currency)
	}
	// Format via big.Int so math.MinInt64 needs no special casing.
	abs := new(big.Int).Abs(big.NewInt(m.Amount))
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)
	major, minor := new(big.Int).QuoRem(abs, scale, new(big.Int))
	sign := ""
	if m.Amount < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%0*d %s", sign, major.String(), exp, minor.Int64(), m.Currency)
}
