package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Currency is an ISO 4217 currency and its minor-unit exponent.
type Currency struct {
	Code        string
	NumericCode int
	Name        string
	MinorUnits  int32
}

// Currencies lists all supported currencies by code.
func (s *Service) Currencies(ctx context.Context) ([]Currency, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, numeric_code, name, minor_units
		FROM currencies
		ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	defer rows.Close()

	var currencies []Currency
	for rows.Next() {
		currency, err := scanCurrency(rows)
		if err != nil {
			return nil, err
		}
		currencies = append(currencies, currency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate currencies: %w", err)
	}
	return currencies, nil
}

func (s *Service) currencyByCode(ctx context.Context, code string) (Currency, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT code, numeric_code, name, minor_units
		FROM currencies
		WHERE code = $1`, code)
	currency, err := scanCurrency(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Currency{}, errUnknownCurrency
	}
	if err != nil {
		return Currency{}, err
	}
	return currency, nil
}

func (s *Service) defaultCurrencyCode(ctx context.Context, householdID uuid.UUID, kind Kind) (string, error) {
	column := "default_spending_currency_code"
	if kind == KindIncome {
		column = "default_income_currency_code"
	}

	var code string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		"SELECT %s FROM household_options WHERE household_id = $1", column,
	), householdID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("household options not found")
	}
	if err != nil {
		return "", fmt.Errorf("get default currency: %w", err)
	}
	return strings.ToUpper(code), nil
}

func scanCurrency(row pgx.Row) (Currency, error) {
	var currency Currency
	var minorUnits int16
	if err := row.Scan(&currency.Code, &currency.NumericCode, &currency.Name, &minorUnits); err != nil {
		return Currency{}, err
	}
	currency.MinorUnits = int32(minorUnits)
	return currency, nil
}
