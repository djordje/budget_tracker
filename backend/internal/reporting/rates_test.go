package reporting

import (
	"math/big"
	"testing"
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
	if err != nil {
		t.Fatalf("NewRateSet() error = %v", err)
	}

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
			if ok != tt.wantOK {
				t.Fatalf("Rate() ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.RatString() != tt.want {
				t.Errorf("Rate() = %s, want %s", got.RatString(), tt.want)
			}
		})
	}
}

func TestRateSetExactDirectBeatsExactInverse(t *testing.T) {
	date := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	set, err := NewRateSet([]finance.ExchangeRate{
		testRate("EUR", "USD", "1.10", date, false),
		testRate("USD", "EUR", "0.85", date, false),
	})
	if err != nil {
		t.Fatalf("NewRateSet() error = %v", err)
	}

	rate, ok := set.Rate("EUR", "USD", date)
	if !ok || rate.RatString() != "11/10" {
		t.Errorf("direct rate = %s, %v; want 11/10", rate.RatString(), ok)
	}
	rate, ok = set.Rate("USD", "EUR", date)
	if !ok || rate.RatString() != "17/20" {
		t.Errorf("reverse direct rate = %s, %v; want 17/20", rate.RatString(), ok)
	}
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
		{name: "2.545 rate one same units", amount: 2545, fromUnits: 2, toUnits: 2, rate: one, want: 2545},
		{name: "direct rate", amount: 10000, fromUnits: 2, toUnits: 2, rate: big.NewRat(100, 1), want: 1_000_000},
		{name: "inverse rate", amount: 1_000_000, fromUnits: 2, toUnits: 2, rate: big.NewRat(1, 100), want: 10000},
		{name: "zero exponent to two", amount: 100, fromUnits: 0, toUnits: 2, rate: big.NewRat(1, 100), want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertMinor(tt.amount, tt.fromUnits, tt.toUnits, tt.rate)
			if err != nil {
				t.Fatalf("ConvertMinor() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ConvertMinor() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConvertMinorRejectsInvalidInput(t *testing.T) {
	if _, err := ConvertMinor(100, 2, 2, nil); err == nil {
		t.Error("ConvertMinor(nil rate) error = nil")
	}
	if _, err := ConvertMinor(100, 2, 2, big.NewRat(0, 1)); err == nil {
		t.Error("ConvertMinor(zero rate) error = nil")
	}
	if _, err := ConvertMinor(-1, 2, 2, big.NewRat(1, 1)); err == nil {
		t.Error("ConvertMinor(negative amount) error = nil")
	}
}
