// Package reporting converts entries into a display currency and summarizes
// totals without silently omitting entries whose rates are missing.
package reporting

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/shopspring/decimal"

	"budgettracker/internal/finance"
)

const (
	exactDateLayout = "2006-01-02"
	monthLayout     = "2006-01"
)

type ratePair struct {
	from string
	to   string
}

// RateSet holds direct exact-date and monthly household rates. Inverse rates
// are inferred only at lookup time, and conversion never chains currencies.
type RateSet struct {
	exact   map[ratePair]map[string]*big.Rat
	monthly map[ratePair]map[string]*big.Rat
}

// NewRateSet builds a rate index from stored exchange rates.
func NewRateSet(rates []finance.ExchangeRate) (RateSet, error) {
	set := RateSet{
		exact:   make(map[ratePair]map[string]*big.Rat),
		monthly: make(map[ratePair]map[string]*big.Rat),
	}
	for _, rate := range rates {
		value, err := decimalStringToRat(rate.Rate)
		if err != nil {
			return RateSet{}, fmt.Errorf("parse exchange rate %d: %w", rate.ID, err)
		}
		pair := ratePair{from: rate.FromCurrencyCode, to: rate.ToCurrencyCode}
		if rate.IsMonthly {
			addRate(set.monthly, pair, rate.RateDate.Format(monthLayout), value)
		} else {
			addRate(set.exact, pair, rate.RateDate.Format(exactDateLayout), value)
		}
	}
	return set, nil
}

func addRate(index map[ratePair]map[string]*big.Rat, pair ratePair, date string, rate *big.Rat) {
	byDate, ok := index[pair]
	if !ok {
		byDate = make(map[string]*big.Rat)
		index[pair] = byDate
	}
	byDate[date] = rate
}

// Rate resolves the rate for a from/to pair and entry date. Exact-date rates
// take precedence over monthly rates; direct rates take precedence over
// inferred inverses at the same scope and date.
func (s RateSet) Rate(from, to string, date time.Time) (*big.Rat, bool) {
	if from == to {
		return big.NewRat(1, 1), true
	}
	dateKey := date.Format(exactDateLayout)
	monthKey := date.Format(monthLayout)
	pair := ratePair{from: from, to: to}
	reverse := ratePair{from: to, to: from}

	if rate, ok := s.exact[pair][dateKey]; ok {
		return copyRat(rate), true
	}
	if rate, ok := s.exact[reverse][dateKey]; ok {
		return inverse(rate), true
	}
	if rate, ok := s.monthly[pair][monthKey]; ok {
		return copyRat(rate), true
	}
	if rate, ok := s.monthly[reverse][monthKey]; ok {
		return inverse(rate), true
	}
	return nil, false
}

func copyRat(value *big.Rat) *big.Rat {
	return new(big.Rat).Set(value)
}

func inverse(value *big.Rat) *big.Rat {
	return new(big.Rat).Inv(value)
}

func decimalStringToRat(raw string) (*big.Rat, error) {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return nil, err
	}
	coefficient := value.Coefficient()
	exponent := value.Exponent()
	rat := new(big.Rat).SetInt(coefficient)
	if exponent < 0 {
		rat.Quo(rat, new(big.Rat).SetInt(pow10(-exponent)))
	} else if exponent > 0 {
		rat.Mul(rat, new(big.Rat).SetInt(pow10(exponent)))
	}
	return rat, nil
}

// ConvertMinor converts an amount in a source currency's minor units to a
// display currency's minor units. The rate is applied exactly and the result
// is rounded half-even only at the final minor-unit precision.
func ConvertMinor(amountMinor int64, fromMinorUnits, toMinorUnits int32, rate *big.Rat) (int64, error) {
	if rate == nil || rate.Sign() <= 0 {
		return 0, errors.New("exchange rate must be positive")
	}
	if amountMinor < 0 {
		return 0, errors.New("entry amount must be positive")
	}

	value := new(big.Rat).SetInt64(amountMinor)
	if fromMinorUnits > 0 {
		value.Quo(value, new(big.Rat).SetInt(pow10(fromMinorUnits)))
	}
	value.Mul(value, rate)
	if toMinorUnits > 0 {
		value.Mul(value, new(big.Rat).SetInt(pow10(toMinorUnits)))
	}
	return roundHalfEven(value)
}

func roundHalfEven(value *big.Rat) (int64, error) {
	numerator := value.Num()
	denominator := value.Denom()
	quotient := new(big.Int).Quo(numerator, denominator)
	remainder := new(big.Int).Rem(numerator, denominator)
	twice := new(big.Int).Lsh(remainder, 1)
	comparison := twice.Cmp(denominator)
	if comparison > 0 || (comparison == 0 && quotient.Bit(0) == 1) {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("converted amount is too large")
	}
	return quotient.Int64(), nil
}

func pow10(exponent int32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}
