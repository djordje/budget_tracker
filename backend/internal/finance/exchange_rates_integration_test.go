//go:build integration

package finance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shopspring/decimal"
)

func TestExchangeRateCRUDAndPrecedenceData(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	rate, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "eur",
		ToCurrencyCode:   "RSD",
		Rate:             "117.25",
		RateDate:         date(2026, 10, 15),
	})
	require.NoError(t, err, "CreateExchangeRate() error")
	assert.Equal(t, "EUR", rate.FromCurrencyCode, "from currency")
	assert.Equal(t, "RSD", rate.ToCurrencyCode, "to currency")
	assert.True(t, decimal.RequireFromString(rate.Rate).Equal(decimal.RequireFromString("117.25")), "rate = %s", rate.Rate)
	assert.False(t, rate.IsMonthly, "exact rate marked monthly")

	monthly, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.member.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "100",
		RateDate:         date(2026, 10, 22),
		IsMonthly:        true,
	})
	require.NoError(t, err, "member monthly CreateExchangeRate() error")
	assert.True(t, monthly.IsMonthly, "monthly rate = %+v", monthly)
	assert.Equal(t, "2026-10-01", monthly.RateDate.Format(entryDateLayout), "monthly rate date")

	rates, err := fixture.service.ListExchangeRates(ctx, fixture.member.User.ID, fixture.householdID)
	require.NoError(t, err, "ListExchangeRates() error")
	require.Len(t, rates, 2, "rate count")

	_, err = fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "118",
		RateDate:         date(2026, 10, 15),
	})
	assert.ErrorIs(t, err, ErrRateExists, "duplicate exact rate")

	_, err = fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "101",
		RateDate:         date(2026, 10, 2),
		IsMonthly:        true,
	})
	assert.ErrorIs(t, err, ErrRateExists, "duplicate monthly rate")

	// Exact and monthly rates for the same pair/date may coexist.
	if _, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "99",
		RateDate:         date(2026, 10, 1),
	}); err != nil {
		assert.NoError(t, err, "exact rate alongside monthly rate")
	}

	_, err = fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.outsider.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "1",
		RateDate:         date(2026, 10, 1),
	})
	assert.ErrorIs(t, err, ErrNotFound, "outsider create rate")

	reversed, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "RSD",
		ToCurrencyCode:   "EUR",
		Rate:             "0.0085",
		RateDate:         date(2026, 10, 15),
	})
	require.NoError(t, err, "reverse rate error")

	newRate := "120.50"
	updated, err := fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
		ID: rate.ID, ActorID: fixture.member.User.ID, Rate: &newRate,
	})
	require.NoError(t, err, "UpdateExchangeRate() error")
	assert.True(t, decimal.RequireFromString(updated.Rate).Equal(decimal.RequireFromString("120.50")), "updated rate = %s", updated.Rate)

	if err := fixture.service.DeleteExchangeRate(ctx, fixture.member.User.ID, reversed.ID); err != nil {
		require.NoError(t, err, "DeleteExchangeRate()")
	}

	err = fixture.service.DeleteExchangeRate(ctx, fixture.outsider.User.ID, rate.ID)
	assert.ErrorIs(t, err, ErrNotFound, "outsider delete")

	assert.NoError(t, fixture.service.DeleteExchangeRate(ctx, fixture.member.User.ID, rate.ID), "member delete admin rate")
}

func TestExchangeRateValidation(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		param CreateExchangeRateParams
		field string
	}{
		{
			name:  "missing pair",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Rate: "1", RateDate: date(2026, 10, 1)},
			field: "from_currency_code",
		},
		{
			name:  "same currencies",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "EUR", ToCurrencyCode: "EUR", Rate: "1", RateDate: date(2026, 10, 1)},
			field: "to_currency_code",
		},
		{
			name:  "unknown source",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "XXX", ToCurrencyCode: "RSD", Rate: "1", RateDate: date(2026, 10, 1)},
			field: "from_currency_code",
		},
		{
			name:  "unknown target",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "EUR", ToCurrencyCode: "XXX", Rate: "1", RateDate: date(2026, 10, 1)},
			field: "to_currency_code",
		},
		{
			name:  "zero rate",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "EUR", ToCurrencyCode: "RSD", Rate: "0", RateDate: date(2026, 10, 1)},
			field: "rate",
		},
		{
			name:  "too much precision",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "EUR", ToCurrencyCode: "RSD", Rate: "1.123456789", RateDate: date(2026, 10, 1)},
			field: "rate",
		},
		{
			name:  "missing date",
			param: CreateExchangeRateParams{HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, FromCurrencyCode: "EUR", ToCurrencyCode: "RSD", Rate: "1"},
			field: "rate_date",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fixture.service.CreateExchangeRate(ctx, tt.param)
			require.Error(t, err, "CreateExchangeRate()")
			var validation *ValidationError
			require.ErrorAs(t, err, &validation, "CreateExchangeRate()")
			assert.Contains(t, validation.Fields, tt.field, "fields = %v", validation.Fields)
		})
	}
}

// TestOversizedRateLeavesNoPartialWrite checks that a rate the schema cannot
// store is rejected before any statement runs: a rejected create inserts no
// row, and a rejected update applies none of its other changes. numeric(18,8)
// holds only rates below 10^10, which the service enforces first.
func TestOversizedRateLeavesNoPartialWrite(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	oversized := "10000000000"

	_, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             oversized,
		RateDate:         date(2026, 10, 15),
	})
	require.Error(t, err, "CreateExchangeRate() with an oversized rate")
	var validation *ValidationError
	require.ErrorAs(t, err, &validation, "CreateExchangeRate() with an oversized rate")
	assert.Contains(t, validation.Fields, "rate", "fields = %v", validation.Fields)

	var rows int
	require.NoError(t, fixture.pool.QueryRow(ctx,
		"SELECT count(*) FROM exchange_rates WHERE household_id = $1", fixture.householdID,
	).Scan(&rows), "count exchange rates")
	assert.Zero(t, rows, "rejected create inserted a rate")

	created, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "117.25",
		RateDate:         date(2026, 10, 15),
	})
	require.NoError(t, err, "CreateExchangeRate() error")

	otherDate := date(2026, 11, 20)
	monthly := true
	_, err = fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
		ID:        created.ID,
		ActorID:   fixture.admin.User.ID,
		Rate:      &oversized,
		RateDate:  &otherDate,
		IsMonthly: &monthly,
	})
	require.Error(t, err, "UpdateExchangeRate() with an oversized rate")
	var updateValidation *ValidationError
	require.ErrorAs(t, err, &updateValidation, "UpdateExchangeRate() with an oversized rate")
	assert.Contains(t, updateValidation.Fields, "rate", "fields = %v", updateValidation.Fields)

	stored, err := fixture.service.ListExchangeRates(ctx, fixture.admin.User.ID, fixture.householdID)
	require.NoError(t, err, "ListExchangeRates() error")
	require.Len(t, stored, 1, "rate rows after a rejected update")
	assert.Equal(t, created.Rate, stored[0].Rate, "rate after rejected update")
	assert.Equal(t, created.RateDate, stored[0].RateDate, "rate date after rejected update")
	assert.Equal(t, created.IsMonthly, stored[0].IsMonthly, "monthly flag after rejected update")
	assert.Equal(t, created.UpdatedAt, stored[0].UpdatedAt, "updated_at after rejected update")
}

func TestExchangeRateIdentifierBoundaries(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	rate, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "117.5",
		RateDate:         date(2026, 10, 1),
	})
	require.NoError(t, err, "CreateExchangeRate() error")
	assert.Positive(t, rate.ID, "rate ID")

	for _, id := range []int64{0, -1} {
		err := fixture.service.DeleteExchangeRate(ctx, fixture.admin.User.ID, id)
		assert.ErrorIs(t, err, ErrNotFound, "DeleteExchangeRate(%d)", id)
		_, err = fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
			ID:      id,
			ActorID: fixture.admin.User.ID,
			Rate:    strPtr("1"),
		})
		assert.ErrorIs(t, err, ErrNotFound, "UpdateExchangeRate(%d)", id)
	}
}

func TestExchangeRateUpdateMapsVanishedRowToNotFound(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	created, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "117.25",
		RateDate:         date(2026, 10, 15),
	})
	require.NoError(t, err, "CreateExchangeRate() error")

	vanishUpdatedRow(t, fixture.pool, "exchange_rates")

	_, err = fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
		ID:      created.ID,
		ActorID: fixture.member.User.ID,
		Rate:    strPtr("118"),
	})
	assert.ErrorIs(t, err, ErrNotFound, "update of a rate deleted between load and write")

	var remaining int
	require.NoError(t, fixture.pool.QueryRow(ctx,
		"SELECT count(*) FROM exchange_rates WHERE id = $1", created.ID,
	).Scan(&remaining), "count exchange rates")
	assert.Equal(t, 0, remaining, "row deleted by the trigger is still stored")
}
