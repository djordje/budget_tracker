package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"budgettracker/internal/store"
)

// ErrRateExists means an exact or monthly rate already exists for the pair and
// applicable date.
var ErrRateExists = errors.New("exchange rate already exists")

// ExchangeRate is a household-specific currency conversion rate.
type ExchangeRate struct {
	ID               int64
	HouseholdID      uuid.UUID
	FromCurrencyCode string
	ToCurrencyCode   string
	Rate             string
	RateDate         time.Time
	IsMonthly        bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RateString formats the stored decimal without insignificant trailing zeroes.
func (r ExchangeRate) RateString() string {
	whole, fraction, found := strings.Cut(r.Rate, ".")
	if !found {
		return r.Rate
	}
	fraction = strings.TrimRight(fraction, "0")
	if fraction == "" {
		return whole
	}
	return whole + "." + fraction
}

// CreateExchangeRateParams contains the input for a new rate.
type CreateExchangeRateParams struct {
	HouseholdID      uuid.UUID
	ActorID          uuid.UUID
	FromCurrencyCode string
	ToCurrencyCode   string
	Rate             string
	RateDate         time.Time
	IsMonthly        bool
}

// UpdateExchangeRateParams contains optional rate changes.
type UpdateExchangeRateParams struct {
	ID               int64
	ActorID          uuid.UUID
	FromCurrencyCode *string
	ToCurrencyCode   *string
	Rate             *string
	RateDate         *time.Time
	IsMonthly        *bool
}

// ListExchangeRates returns the household's rates to a member.
func (s *Service) ListExchangeRates(ctx context.Context, actorID, householdID uuid.UUID) ([]ExchangeRate, error) {
	return s.ListExchangeRatesTx(ctx, s.pool, actorID, householdID)
}

// ListExchangeRatesTx reads rates through the provided query source.
func (s *Service) ListExchangeRatesTx(ctx context.Context, db Querier, actorID, householdID uuid.UUID) ([]ExchangeRate, error) {
	if _, err := s.membershipTx(ctx, db, householdID, actorID); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT id, household_id, from_currency_code, to_currency_code, rate::text,
		       rate_date, is_monthly, created_at, updated_at
		FROM exchange_rates
		WHERE household_id = $1
		ORDER BY from_currency_code, to_currency_code, is_monthly, rate_date`,
		householdID,
	)
	if err != nil {
		return nil, fmt.Errorf("list exchange rates: %w", err)
	}
	defer rows.Close()

	var rates []ExchangeRate
	for rows.Next() {
		rate, err := scanExchangeRate(rows)
		if err != nil {
			return nil, err
		}
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exchange rates: %w", err)
	}
	return rates, nil
}

// CreateExchangeRate stores a rate for any household member.
func (s *Service) CreateExchangeRate(ctx context.Context, params CreateExchangeRateParams) (ExchangeRate, error) {
	if _, err := s.membership(ctx, params.HouseholdID, params.ActorID); err != nil {
		return ExchangeRate{}, err
	}

	from, to, rate, rateDate, isMonthly, validation, err := s.validateRateFields(
		ctx,
		params.FromCurrencyCode,
		params.ToCurrencyCode,
		params.Rate,
		params.RateDate,
		params.IsMonthly,
	)
	if err != nil {
		return ExchangeRate{}, err
	}
	if validation != nil {
		return ExchangeRate{}, validation
	}

	var created ExchangeRate
	err = s.pool.QueryRow(ctx, `
		INSERT INTO exchange_rates
			(household_id, from_currency_code, to_currency_code, rate, rate_date, is_monthly)
		VALUES ($1, $2, $3, $4, $5::date, $6)
		RETURNING id, household_id, from_currency_code, to_currency_code, rate::text,
		          rate_date, is_monthly, created_at, updated_at`,
		params.HouseholdID, from, to, rate, rateDate.Format(entryDateLayout), isMonthly,
	).Scan(
		&created.ID, &created.HouseholdID, &created.FromCurrencyCode, &created.ToCurrencyCode,
		&created.Rate, &created.RateDate, &created.IsMonthly, &created.CreatedAt, &created.UpdatedAt,
	)
	if store.IsUniqueViolation(err, "exchange_rates_exact_unique") ||
		store.IsUniqueViolation(err, "exchange_rates_monthly_unique") {
		return ExchangeRate{}, ErrRateExists
	}
	if err != nil {
		return ExchangeRate{}, fmt.Errorf("insert exchange rate: %w", err)
	}
	return created, nil
}

// UpdateExchangeRate changes an existing rate for a household member.
func (s *Service) UpdateExchangeRate(ctx context.Context, params UpdateExchangeRateParams) (ExchangeRate, error) {
	current, err := s.loadExchangeRate(ctx, params.ID)
	if err != nil {
		return ExchangeRate{}, err
	}
	if _, err := s.membership(ctx, current.HouseholdID, params.ActorID); err != nil {
		return ExchangeRate{}, err
	}

	from := current.FromCurrencyCode
	to := current.ToCurrencyCode
	rate := current.Rate
	rateDate := current.RateDate
	isMonthly := current.IsMonthly
	if params.FromCurrencyCode != nil {
		from = strings.ToUpper(strings.TrimSpace(*params.FromCurrencyCode))
	}
	if params.ToCurrencyCode != nil {
		to = strings.ToUpper(strings.TrimSpace(*params.ToCurrencyCode))
	}
	if params.Rate != nil {
		rate = *params.Rate
	}
	if params.RateDate != nil {
		rateDate = *params.RateDate
	}
	if params.IsMonthly != nil {
		isMonthly = *params.IsMonthly
	}

	from, to, rate, rateDate, isMonthly, validation, err := s.validateRateFields(
		ctx, from, to, rate, rateDate, isMonthly,
	)
	if err != nil {
		return ExchangeRate{}, err
	}
	if validation != nil {
		return ExchangeRate{}, validation
	}

	var updated ExchangeRate
	err = s.pool.QueryRow(ctx, `
		UPDATE exchange_rates
		SET from_currency_code = $2, to_currency_code = $3, rate = $4,
		    rate_date = $5::date, is_monthly = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, household_id, from_currency_code, to_currency_code, rate::text,
		          rate_date, is_monthly, created_at, updated_at`,
		current.ID, from, to, rate, rateDate.Format(entryDateLayout), isMonthly,
	).Scan(
		&updated.ID, &updated.HouseholdID, &updated.FromCurrencyCode, &updated.ToCurrencyCode,
		&updated.Rate, &updated.RateDate, &updated.IsMonthly, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if store.IsUniqueViolation(err, "exchange_rates_exact_unique") ||
		store.IsUniqueViolation(err, "exchange_rates_monthly_unique") {
		return ExchangeRate{}, ErrRateExists
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ExchangeRate{}, ErrNotFound
	}
	if err != nil {
		return ExchangeRate{}, fmt.Errorf("update exchange rate: %w", err)
	}
	return updated, nil
}

// DeleteExchangeRate removes a rate for a household member.
func (s *Service) DeleteExchangeRate(ctx context.Context, actorID uuid.UUID, rateID int64) error {
	current, err := s.loadExchangeRate(ctx, rateID)
	if err != nil {
		return err
	}
	if _, err := s.membership(ctx, current.HouseholdID, actorID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM exchange_rates WHERE id = $1", rateID); err != nil {
		return fmt.Errorf("delete exchange rate: %w", err)
	}
	return nil
}

func (s *Service) loadExchangeRate(ctx context.Context, id int64) (ExchangeRate, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, household_id, from_currency_code, to_currency_code, rate::text,
		       rate_date, is_monthly, created_at, updated_at
		FROM exchange_rates
		WHERE id = $1`, id)
	rate, err := scanExchangeRate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExchangeRate{}, ErrNotFound
	}
	if err != nil {
		return ExchangeRate{}, err
	}
	return rate, nil
}

func scanExchangeRate(row pgx.Row) (ExchangeRate, error) {
	var rate ExchangeRate
	if err := row.Scan(
		&rate.ID, &rate.HouseholdID, &rate.FromCurrencyCode, &rate.ToCurrencyCode,
		&rate.Rate, &rate.RateDate, &rate.IsMonthly, &rate.CreatedAt, &rate.UpdatedAt,
	); err != nil {
		return ExchangeRate{}, err
	}
	return rate, nil
}

func (s *Service) validateRateFields(
	ctx context.Context,
	fromRaw, toRaw, rateRaw string,
	rateDate time.Time,
	isMonthly bool,
) (string, string, string, time.Time, bool, *ValidationError, error) {
	validation := newValidationError()
	from := strings.ToUpper(strings.TrimSpace(fromRaw))
	to := strings.ToUpper(strings.TrimSpace(toRaw))

	switch {
	case from == "":
		validation.add("from_currency_code", "Choose a source currency.")
	case to == "":
		validation.add("to_currency_code", "Choose a target currency.")
	case from == to:
		validation.add("to_currency_code", "Choose a different target currency.")
	}
	if from != "" {
		if _, err := s.currencyByCode(ctx, from); errors.Is(err, errUnknownCurrency) {
			validation.add("from_currency_code", "Choose a supported currency.")
		} else if err != nil {
			return "", "", "", time.Time{}, false, nil, err
		}
	}
	if to != "" {
		if _, err := s.currencyByCode(ctx, to); errors.Is(err, errUnknownCurrency) {
			validation.add("to_currency_code", "Choose a supported currency.")
		} else if err != nil {
			return "", "", "", time.Time{}, false, nil, err
		}
	}

	rate, rateMessage := canonicalRate(rateRaw)
	if rateMessage != nil {
		validation.add("rate", rateMessage.text)
	}
	if rateDate.IsZero() {
		validation.add("rate_date", "Enter a valid rate date.")
	} else if isMonthly {
		rateDate = monthStart(rateDate)
	} else {
		rateDate = dateOnly(rateDate)
	}

	if len(validation.Fields) > 0 {
		return "", "", "", time.Time{}, false, validation, nil
	}
	return from, to, rate, rateDate, isMonthly, nil, nil
}

var (
	positiveRateMessage = &validationMessage{text: "Enter a positive rate with at most 8 decimal places."}
	rateScaleMessage    = &validationMessage{text: "Enter a rate with at most 8 decimal places."}
	largeRateMessage    = &validationMessage{text: "Enter a smaller rate."}
)

// rateDecimalPlaces is the fractional precision rates are stored at
// (numeric(18,8)).
const rateDecimalPlaces = 8

// canonicalRate returns the stored form of a rate. Only plain decimal notation
// is accepted, matching the form copy, and the value may not need more than
// rateDecimalPlaces decimals: exactly representable values with extra trailing
// zeros are canonicalized instead of rejected because numeric(18,8) stores them
// exactly.
func canonicalRate(raw string) (string, *validationMessage) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", positiveRateMessage
	}
	if strings.ContainsAny(value, "eE") {
		return "", rateScaleMessage
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || !parsed.IsPositive() {
		return "", positiveRateMessage
	}
	if !parsed.Equal(parsed.Round(rateDecimalPlaces)) {
		return "", rateScaleMessage
	}
	if parsed.Cmp(decimal.NewFromInt(10_000_000_000)) >= 0 {
		return "", largeRateMessage
	}
	return parsed.String(), nil
}

func monthStart(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}
