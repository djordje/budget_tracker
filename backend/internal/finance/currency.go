package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// recentCurrencyWindow is how many recorded entries the recents query inspects
// per entry kind before deduplicating.
const recentCurrencyWindow = 20

// Currency is an ISO 4217 currency and its minor-unit exponent.
type Currency struct {
	Code        string
	NumericCode int
	Name        string
	MinorUnits  int32
}

// Currencies lists all supported currencies by code.
func (s *Service) Currencies(ctx context.Context) ([]Currency, error) {
	return s.CurrenciesTx(ctx, s.pool)
}

// CurrenciesTx lists supported currencies using the provided query source.
func (s *Service) CurrenciesTx(ctx context.Context, db Querier) ([]Currency, error) {
	rows, err := db.Query(ctx, `
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
	return s.currencyByCodeTx(ctx, s.pool, code)
}

func (s *Service) currencyByCodeTx(ctx context.Context, db Querier, code string) (Currency, error) {
	row := db.QueryRow(ctx, `
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

// RecentCurrencies returns the currency codes the actor used most recently for
// their own entries in one household, most recently recorded first, with each
// code appearing once. Other members' entries and household-wide entries never
// contribute, even for admins. Entries count by recording time, not entry date.
func (s *Service) RecentCurrencies(ctx context.Context, householdID, actorID uuid.UUID) ([]string, error) {
	if _, err := s.membership(ctx, householdID, actorID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT currency_code
		FROM (
			(SELECT currency_code, created_at, id, 0 AS kind_rank
			 FROM incomes
			 WHERE household_id = $1 AND user_id = $2
			 ORDER BY created_at DESC, id DESC
			 LIMIT $3)
			UNION ALL
			(SELECT currency_code, created_at, id, 1 AS kind_rank
			 FROM spendings
			 WHERE household_id = $1 AND user_id = $2
			 ORDER BY created_at DESC, id DESC
			 LIMIT $3)
		) recent
		ORDER BY created_at DESC, kind_rank, id DESC
		LIMIT $3`,
		householdID, actorID, recentCurrencyWindow,
	)
	if err != nil {
		return nil, fmt.Errorf("recent currencies: %w", err)
	}
	defer rows.Close()

	codes := make([]string, 0, recentCurrencyWindow)
	seen := make(map[string]bool, recentCurrencyWindow)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan recent currency: %w", err)
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent currencies: %w", err)
	}
	return codes, nil
}

// OrderCurrencies orders entry-form currency choices for relevance: the
// selected code first when the catalog contains it, then the recently used
// codes in the order given, then the remaining catalog in catalog order. Codes
// outside the catalog are ignored, so every supported currency stays available.
func OrderCurrencies(catalog []Currency, selected string, recent []string) []Currency {
	byCode := make(map[string]Currency, len(catalog))
	for _, currency := range catalog {
		byCode[currency.Code] = currency
	}

	ordered := make([]Currency, 0, len(catalog))
	seen := make(map[string]bool, len(catalog))
	appendCode := func(code string) {
		if code == "" || seen[code] {
			return
		}
		currency, ok := byCode[code]
		if !ok {
			return
		}
		seen[code] = true
		ordered = append(ordered, currency)
	}

	appendCode(selected)
	for _, code := range recent {
		appendCode(code)
	}
	for _, currency := range catalog {
		appendCode(currency.Code)
	}
	return ordered
}

func (s *Service) defaultCurrencyCode(ctx context.Context, householdID, userID uuid.UUID, kind Kind) (string, error) {
	column := "default_spending_currency_code"
	if kind == KindIncome {
		column = "default_income_currency_code"
	}

	var code string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COALESCE(u.%s, h.%s)
		FROM household_options h
		LEFT JOIN user_options u
		  ON u.household_id = h.household_id AND u.user_id = $2
		WHERE h.household_id = $1`, column, column),
		householdID, userID,
	).Scan(&code)
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
