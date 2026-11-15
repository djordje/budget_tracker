package reporting

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/finance"
)

// SummaryParams describes a household summary request.
type SummaryParams struct {
	HouseholdID     uuid.UUID
	ActorID         uuid.UUID
	Scope           finance.Scope
	From            time.Time
	To              time.Time
	DisplayCurrency string
}

// MissingRate identifies a currency pair and entry date that could not be
// converted.
type MissingRate struct {
	FromCurrencyCode string
	ToCurrencyCode   string
	EntryDate        string
}

// Summary is a converted household total. Totals include only entries that
// could be converted; Complete is false and MissingRates is populated when
// any entry was skipped. A conversion or total that overflows int64 is not an
// incompleteness: the service returns an error and no summary, because the
// value cannot be represented at all.
type Summary struct {
	From               string
	To                 string
	Scope              finance.Scope
	DisplayCurrency    string
	DisplayMinorUnits  int32
	IncomeMinor        int64
	SpendingMinor      int64
	NetMinor           int64
	EntryCount         int
	IncludedEntryCount int
	MissingEntryCount  int
	Complete           bool
	MissingRates       []MissingRate
}

// IncomeString formats income without grouping for APIs.
func (s Summary) IncomeString() string {
	return finance.FormatAmount(s.IncomeMinor, s.DisplayMinorUnits)
}

// SpendingString formats spending without grouping for APIs.
func (s Summary) SpendingString() string {
	return finance.FormatAmount(s.SpendingMinor, s.DisplayMinorUnits)
}

// NetString formats net without grouping for APIs.
func (s Summary) NetString() string {
	return finance.FormatAmount(s.NetMinor, s.DisplayMinorUnits)
}

// DisplayIncome formats income with grouped thousands for HTML.
func (s Summary) DisplayIncome() string {
	return finance.DisplayAmount(s.IncomeMinor, s.DisplayMinorUnits)
}

// DisplaySpending formats spending with grouped thousands for HTML.
func (s Summary) DisplaySpending() string {
	return finance.DisplayAmount(s.SpendingMinor, s.DisplayMinorUnits)
}

// DisplayNet formats net with grouped thousands for HTML.
func (s Summary) DisplayNet() string {
	return finance.DisplayAmount(s.NetMinor, s.DisplayMinorUnits)
}

// Service computes converted summaries from entries and household rates.
type Service struct {
	pool    *pgxpool.Pool
	entries *finance.Service
}

// NewService constructs a reporting service.
func NewService(pool *pgxpool.Pool, entries *finance.Service) *Service {
	return &Service{pool: pool, entries: entries}
}

// Summary converts entries for a date range into the display currency and
// reports any missing rates instead of silently dropping entries.
func (s *Service) Summary(ctx context.Context, params SummaryParams) (Summary, error) {
	if params.Scope == "" {
		params.Scope = finance.ScopeMine
	}
	if params.From.IsZero() || params.To.IsZero() {
		return Summary{}, validationError("from", "Choose a valid date range.")
	}
	from := dateOnly(params.From)
	to := dateOnly(params.To)
	if from.After(to) {
		return Summary{}, validationError("from", "The start date must not be after the end date.")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("begin report transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	display, err := s.displayCurrency(ctx, tx, params.HouseholdID, params.ActorID, params.DisplayCurrency)
	if err != nil {
		return Summary{}, err
	}

	entries, err := s.entries.EntriesForRangeTx(ctx, tx, params.HouseholdID, params.ActorID, params.Scope, from, to)
	if err != nil {
		return Summary{}, err
	}
	rateRecords, err := s.entries.ListExchangeRatesTx(ctx, tx, params.ActorID, params.HouseholdID)
	if err != nil {
		return Summary{}, err
	}
	rateSet, err := NewRateSet(rateRecords)
	if err != nil {
		return Summary{}, fmt.Errorf("build exchange rate set: %w", err)
	}

	summary := Summary{
		From:              from.Format(exactDateLayout),
		To:                to.Format(exactDateLayout),
		Scope:             params.Scope,
		DisplayCurrency:   display.Code,
		DisplayMinorUnits: display.MinorUnits,
		EntryCount:        len(entries),
		MissingRates:      make([]MissingRate, 0),
	}
	missing := make(map[string]MissingRate)
	for _, entry := range entries {
		var converted int64
		if entry.CurrencyCode == display.Code {
			converted = entry.AmountMinor
		} else {
			rate, ok := rateSet.Rate(entry.CurrencyCode, display.Code, entry.EntryDate)
			if !ok {
				key := entry.CurrencyCode + "\x00" + display.Code + "\x00" + entry.EntryDate.Format(exactDateLayout)
				missing[key] = MissingRate{
					FromCurrencyCode: entry.CurrencyCode,
					ToCurrencyCode:   display.Code,
					EntryDate:        entry.EntryDate.Format(exactDateLayout),
				}
				summary.MissingEntryCount++
				continue
			}
			converted, err = ConvertMinor(entry.AmountMinor, entry.CurrencyMinorUnits, display.MinorUnits, rate)
			if err != nil {
				// Overflow is not a missing rate: an unrepresentable total aborts the
				// report instead of being reported as incomplete, because it would be
				// wrong rather than partial.
				return Summary{}, fmt.Errorf("convert %s entry %d: %w", entry.Kind, entry.ID, err)
			}
		}

		switch entry.Kind {
		case finance.KindIncome:
			summary.IncomeMinor, err = addAmount(summary.IncomeMinor, converted)
		case finance.KindSpending:
			summary.SpendingMinor, err = addAmount(summary.SpendingMinor, converted)
		}
		if err != nil {
			return Summary{}, err
		}
		summary.IncludedEntryCount++
	}

	summary.NetMinor = summary.IncomeMinor - summary.SpendingMinor
	summary.Complete = summary.MissingEntryCount == 0
	for _, item := range missing {
		summary.MissingRates = append(summary.MissingRates, item)
	}
	sort.Slice(summary.MissingRates, func(i, j int) bool {
		if summary.MissingRates[i].EntryDate != summary.MissingRates[j].EntryDate {
			return summary.MissingRates[i].EntryDate < summary.MissingRates[j].EntryDate
		}
		if summary.MissingRates[i].FromCurrencyCode != summary.MissingRates[j].FromCurrencyCode {
			return summary.MissingRates[i].FromCurrencyCode < summary.MissingRates[j].FromCurrencyCode
		}
		return summary.MissingRates[i].ToCurrencyCode < summary.MissingRates[j].ToCurrencyCode
	})
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("commit report transaction: %w", err)
	}
	return summary, nil
}

func (s *Service) displayCurrency(
	ctx context.Context,
	db finance.Querier,
	householdID, userID uuid.UUID,
	raw string,
) (finance.Currency, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		err := db.QueryRow(ctx, `
			SELECT COALESCE(u.default_display_currency_code, h.default_display_currency_code)
			FROM household_options h
			LEFT JOIN user_options u
			  ON u.household_id = h.household_id AND u.user_id = $2
			WHERE h.household_id = $1`, householdID, userID).Scan(&code)
		if errors.Is(err, pgx.ErrNoRows) {
			return finance.Currency{}, finance.ErrNotFound
		}
		if err != nil {
			return finance.Currency{}, fmt.Errorf("get default display currency: %w", err)
		}
		code = strings.ToUpper(code)
	}

	currencies, err := s.entries.CurrenciesTx(ctx, db)
	if err != nil {
		return finance.Currency{}, err
	}
	for _, currency := range currencies {
		if currency.Code == code {
			return currency, nil
		}
	}
	return finance.Currency{}, validationError("currency", "Choose a supported display currency.")
}

func addAmount(total, value int64) (int64, error) {
	if value > 0 && total > math.MaxInt64-value {
		return 0, errors.New("summary amount is too large")
	}
	return total + value, nil
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func validationError(field, message string) *finance.ValidationError {
	return &finance.ValidationError{Fields: map[string]string{field: message}}
}
