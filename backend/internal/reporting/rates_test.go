package reporting

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"

	"budgettracker/internal/finance"
)

func testRate(from, to, rate string, date time.Time, monthly bool) finance.ExchangeRate {
	return finance.ExchangeRate{
		FromCurrencyCode: from,
		ToCurrencyCode:   to,
		Rate:             rate,
		RateDate:         date,
		IsMonthly:        monthly,
	}
}

func TestRateSetPrecedence(t *testing.T) {
	date := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	exactDate := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	monthDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	rates := []finance.ExchangeRate{
		testRate("EUR", "USD", "1.10", exactDate, false),
		testRate("USD", "EUR", "0.80", monthDate, true),
		testRate("USD", "RSD", "100", monthDate, true),
	}
	set, err := NewRateSet(rates)
	require.NoError(t, err, "NewRateSet()")

	tests := []struct {
		name   string
		from   string
		to     string
		date   time.Time
		want   string
		wantOK bool
	}{
		{name: "same currency", from: "EUR", to: "EUR", date: date, want: "1", wantOK: true},
		{name: "direct exact", from: "EUR", to: "USD", date: date, want: "11/10", wantOK: true},
		{name: "inverse exact beats monthly direct", from: "USD", to: "EUR", date: date, want: "10/11", wantOK: true},
		{name: "monthly direct", from: "USD", to: "RSD", date: date, want: "100", wantOK: true},
		{name: "monthly inverse", from: "RSD", to: "USD", date: date, want: "1/100", wantOK: true},
		{name: "exact only on its date", from: "EUR", to: "USD", date: date.AddDate(0, 1, 0), wantOK: false},
		{name: "monthly outside month", from: "USD", to: "RSD", date: date.AddDate(0, 1, 0), wantOK: false},
		{name: "no chaining", from: "EUR", to: "RSD", date: date, wantOK: false},
		{name: "missing", from: "GBP", to: "EUR", date: date, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := set.Rate(tt.from, tt.to, tt.date)
			if !tt.wantOK {
				assert.False(t, ok, "Rate() ok")
				return
			}
			require.True(t, ok, "Rate() ok")
			assert.Equal(t, tt.want, got.RatString(), "Rate()")
		})
	}
}

func TestRateSetExactDirectBeatsExactInverse(t *testing.T) {
	date := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	set, err := NewRateSet([]finance.ExchangeRate{
		testRate("EUR", "USD", "1.10", date, false),
		testRate("USD", "EUR", "0.85", date, false),
	})
	require.NoError(t, err, "NewRateSet()")

	rate, ok := set.Rate("EUR", "USD", date)
	require.True(t, ok, "Rate() ok")
	assert.Equal(t, "11/10", rate.RatString(), "direct rate")
	rate, ok = set.Rate("USD", "EUR", date)
	require.True(t, ok, "Rate() ok")
	assert.Equal(t, "17/20", rate.RatString(), "reverse direct rate")
}

func TestConvertMinorRoundsHalfEven(t *testing.T) {
	one := big.NewRat(1, 1)
	tests := []struct {
		name      string
		amount    int64
		fromUnits int32
		toUnits   int32
		rate      *big.Rat
		want      int64
	}{
		{name: "2.545 to 2.54", amount: 2545, fromUnits: 3, toUnits: 2, rate: one, want: 254},
		{name: "2.555 to 2.56", amount: 2555, fromUnits: 3, toUnits: 2, rate: one, want: 256},
		{name: "rounds down below half", amount: 4, fromUnits: 2, toUnits: 2, rate: big.NewRat(1, 10), want: 0},
		{name: "half rounds to even zero", amount: 5, fromUnits: 2, toUnits: 2, rate: big.NewRat(1, 10), want: 0},
		{name: "2.545 rate one same units", amount: 2545, fromUnits: 2, toUnits: 2, rate: one, want: 2545},
		{name: "direct rate", amount: 10000, fromUnits: 2, toUnits: 2, rate: big.NewRat(100, 1), want: 1_000_000},
		{name: "inverse rate", amount: 1_000_000, fromUnits: 2, toUnits: 2, rate: big.NewRat(1, 100), want: 10000},
		{name: "zero exponent to two", amount: 100, fromUnits: 0, toUnits: 2, rate: big.NewRat(1, 100), want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertMinor(tt.amount, tt.fromUnits, tt.toUnits, tt.rate)
			require.NoError(t, err, "ConvertMinor()")
			assert.Equal(t, tt.want, got, "ConvertMinor()")
		})
	}
}

func TestConvertMinorRejectsInvalidInput(t *testing.T) {
	_, err := ConvertMinor(100, 2, 2, nil)
	assert.Error(t, err, "ConvertMinor(nil rate)")

	_, err = ConvertMinor(100, 2, 2, big.NewRat(0, 1))
	assert.Error(t, err, "ConvertMinor(zero rate)")

	_, err = ConvertMinor(-1, 2, 2, big.NewRat(1, 1))
	assert.Error(t, err, "ConvertMinor(negative amount)")
}

// TestConvertMinorRejectsOverflow checks that an exact conversion whose result
// cannot be represented returns an error and no partial amount.
func TestConvertMinorRejectsOverflow(t *testing.T) {
	tests := []struct {
		name      string
		amount    int64
		fromUnits int32
		toUnits   int32
		rate      *big.Rat
	}{
		{
			name:   "largest amount with the largest rate",
			amount: math.MaxInt64,
			rate:   big.NewRat(math.MaxInt64, 1),
		},
		{
			name:      "large amount with a large rate at two decimals",
			amount:    math.MaxInt64,
			fromUnits: 2,
			toUnits:   2,
			rate:      big.NewRat(1_000_000_000, 1),
		},
		{
			name:    "large amount with a large display exponent",
			amount:  1_000_000_000_000_000_000,
			toUnits: 4,
			rate:    big.NewRat(1, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertMinor(tt.amount, tt.fromUnits, tt.toUnits, tt.rate)
			assert.ErrorContains(t, err, "converted amount is too large",
				"ConvertMinor(%d, %d, %d)", tt.amount, tt.fromUnits, tt.toUnits)
			assert.Zero(t, got, "ConvertMinor(%d, %d, %d)", tt.amount, tt.fromUnits, tt.toUnits)
		})
	}
}
