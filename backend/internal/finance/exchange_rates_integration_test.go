//go:build integration

package finance

import (
	"context"
	"errors"
	"testing"

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
	if err != nil {
		t.Fatalf("CreateExchangeRate() error = %v", err)
	}
	if rate.FromCurrencyCode != "EUR" || rate.ToCurrencyCode != "RSD" {
		t.Errorf("pair = %s/%s", rate.FromCurrencyCode, rate.ToCurrencyCode)
	}
	if !decimal.RequireFromString(rate.Rate).Equal(decimal.RequireFromString("117.25")) {
		t.Errorf("rate = %s", rate.Rate)
	}
	if rate.IsMonthly {
		t.Error("exact rate marked monthly")
	}

	monthly, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.member.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "100",
		RateDate:         date(2026, 10, 22),
		IsMonthly:        true,
	})
	if err != nil {
		t.Fatalf("member monthly CreateExchangeRate() error = %v", err)
	}
	if !monthly.IsMonthly || monthly.RateDate.Format(entryDateLayout) != "2026-10-01" {
		t.Errorf("monthly rate = %+v", monthly)
	}

	rates, err := fixture.service.ListExchangeRates(ctx, fixture.member.User.ID, fixture.householdID)
	if err != nil {
		t.Fatalf("ListExchangeRates() error = %v", err)
	}
	if len(rates) != 2 {
		t.Fatalf("rate count = %d, want 2", len(rates))
	}

	if _, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "118",
		RateDate:         date(2026, 10, 15),
	}); !errors.Is(err, ErrRateExists) {
		t.Errorf("duplicate exact error = %v, want ErrRateExists", err)
	}

	if _, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "101",
		RateDate:         date(2026, 10, 2),
		IsMonthly:        true,
	}); !errors.Is(err, ErrRateExists) {
		t.Errorf("duplicate monthly error = %v, want ErrRateExists", err)
	}

	// Exact and monthly rates for the same pair/date may coexist.
	if _, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "USD",
		ToCurrencyCode:   "RSD",
		Rate:             "99",
		RateDate:         date(2026, 10, 1),
	}); err != nil {
		t.Errorf("exact alongside monthly error = %v", err)
	}

	if _, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.outsider.User.ID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "1",
		RateDate:         date(2026, 10, 1),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider create error = %v, want ErrNotFound", err)
	}

	reversed, err := fixture.service.CreateExchangeRate(ctx, CreateExchangeRateParams{
		HouseholdID:      fixture.householdID,
		ActorID:          fixture.admin.User.ID,
		FromCurrencyCode: "RSD",
		ToCurrencyCode:   "EUR",
		Rate:             "0.0085",
		RateDate:         date(2026, 10, 15),
	})
	if err != nil {
		t.Fatalf("reverse rate error = %v", err)
	}

	newRate := "120.50"
	updated, err := fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
		ID: rate.ID, ActorID: fixture.member.User.ID, Rate: &newRate,
	})
	if err != nil {
		t.Fatalf("UpdateExchangeRate() error = %v", err)
	}
	if !decimal.RequireFromString(updated.Rate).Equal(decimal.RequireFromString("120.50")) {
		t.Errorf("updated rate = %s", updated.Rate)
	}

	if err := fixture.service.DeleteExchangeRate(ctx, fixture.member.User.ID, reversed.ID); err != nil {
		t.Fatalf("DeleteExchangeRate() error = %v", err)
	}
	if err := fixture.service.DeleteExchangeRate(ctx, fixture.outsider.User.ID, rate.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider delete error = %v, want ErrNotFound", err)
	}
	if err := fixture.service.DeleteExchangeRate(ctx, fixture.member.User.ID, rate.ID); err != nil {
		t.Errorf("member delete admin rate error = %v", err)
	}
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
			if err == nil {
				t.Fatal("CreateExchangeRate() error = nil, want validation error")
			}
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T, want ValidationError", err)
			}
			if _, ok := validation.Fields[tt.field]; !ok {
				t.Errorf("fields = %v, want %q", validation.Fields, tt.field)
			}
		})
	}
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
	if err != nil {
		t.Fatalf("CreateExchangeRate() error = %v", err)
	}
	if rate.ID <= 0 {
		t.Errorf("rate ID = %d, want a positive integer", rate.ID)
	}

	for _, id := range []int64{0, -1} {
		if err := fixture.service.DeleteExchangeRate(ctx, fixture.admin.User.ID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteExchangeRate(%d) error = %v, want ErrNotFound", id, err)
		}
		if _, err := fixture.service.UpdateExchangeRate(ctx, UpdateExchangeRateParams{
			ID:      id,
			ActorID: fixture.admin.User.ID,
			Rate:    strPtr("1"),
		}); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateExchangeRate(%d) error = %v, want ErrNotFound", id, err)
		}
	}
}
