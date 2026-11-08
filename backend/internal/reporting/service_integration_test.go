//go:build integration

package reporting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
	"budgettracker/internal/testdb"
)

type reportFixture struct {
	pool        *pgxpool.Pool
	finance     *finance.Service
	reporting   *Service
	householdID uuid.UUID
	admin       auth.Session
	member      auth.Session
	outsider    auth.Session
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	pool := testdb.New(t)
	authService := auth.NewService(pool)
	householdService := households.NewService(pool)
	financeService := finance.NewService(pool)
	reportingService := NewService(pool, financeService)
	ctx := context.Background()

	admin := registerReportUser(t, authService, "admin@example.com")
	member := registerReportUser(t, authService, "member@example.com")
	outsider := registerReportUser(t, authService, "outsider@example.com")
	household, err := householdService.CreateAndActivate(ctx, admin.User.ID, admin.TokenHash, "Home")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, household.ID, member.User.ID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	return &reportFixture{
		pool:        pool,
		finance:     financeService,
		reporting:   reportingService,
		householdID: household.ID,
		admin:       admin,
		member:      member,
		outsider:    outsider,
	}
}

func registerReportUser(t *testing.T, service *auth.Service, email string) auth.Session {
	t.Helper()
	session, err := service.Register(context.Background(), auth.RegisterParams{
		FirstName: "Test",
		LastName:  "User",
		Email:     email,
		Password:  "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return session
}

func reportDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func (f *reportFixture) entry(t *testing.T, actor uuid.UUID, kind finance.Kind, amount, currency string, day time.Time, householdWide bool) finance.Entry {
	t.Helper()
	entry, err := f.finance.Create(context.Background(), finance.CreateParams{
		Kind:          kind,
		HouseholdID:   f.householdID,
		ActorID:       actor,
		Amount:        amount,
		CurrencyCode:  currency,
		EntryDate:     day,
		HouseholdWide: householdWide,
	})
	if err != nil {
		t.Fatalf("create %s entry: %v", kind, err)
	}
	return entry
}

func (f *reportFixture) rate(t *testing.T, from, to, value string, day time.Time, monthly bool) {
	t.Helper()
	if _, err := f.finance.CreateExchangeRate(context.Background(), finance.CreateExchangeRateParams{
		HouseholdID:      f.householdID,
		ActorID:          f.admin.User.ID,
		FromCurrencyCode: from,
		ToCurrencyCode:   to,
		Rate:             value,
		RateDate:         day,
		IsMonthly:        monthly,
	}); err != nil {
		t.Fatalf("create exchange rate %s/%s: %v", from, to, err)
	}
}

func TestSummaryConversionScopesAndMissingRates(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := context.Background()

	fixture.rate(t, "EUR", "RSD", "117.5", reportDate(2026, 10, 15), false)
	fixture.rate(t, "USD", "RSD", "100", reportDate(2026, 10, 1), true)

	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "100", "RSD", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "10", "EUR", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.member.User.ID, finance.KindIncome, "20", "USD", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "50", "RSD", reportDate(2026, 10, 15), true)

	params := SummaryParams{
		HouseholdID:     fixture.householdID,
		ActorID:         fixture.admin.User.ID,
		Scope:           finance.ScopeAll,
		From:            reportDate(2026, 10, 1),
		To:              reportDate(2026, 10, 31),
		DisplayCurrency: "RSD",
	}
	summary, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.IncomeMinor != 210000 {
		t.Errorf("income = %d, want 210000", summary.IncomeMinor)
	}
	if summary.SpendingMinor != 122500 {
		t.Errorf("spending = %d, want 122500", summary.SpendingMinor)
	}
	if summary.NetMinor != 87500 {
		t.Errorf("net = %d, want 87500", summary.NetMinor)
	}
	if summary.EntryCount != 4 || summary.IncludedEntryCount != 4 || !summary.Complete {
		t.Errorf("summary completeness = %+v", summary)
	}

	params.ActorID = fixture.admin.User.ID
	params.Scope = finance.ScopeMine
	mine, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("admin mine Summary() error = %v", err)
	}
	if mine.IncomeMinor != 10000 || mine.SpendingMinor != 117500 || mine.EntryCount != 2 {
		t.Errorf("admin mine = %+v, want only personal entries", mine)
	}

	params.ActorID = fixture.member.User.ID
	memberMine, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("member mine Summary() error = %v", err)
	}
	if memberMine.IncomeMinor != 200000 || memberMine.EntryCount != 1 {
		t.Errorf("member mine = %+v", memberMine)
	}

	params.Scope = finance.ScopeAll
	if _, err := fixture.reporting.Summary(ctx, params); !errors.Is(err, finance.ErrForbidden) {
		t.Errorf("member all error = %v, want ErrForbidden", err)
	}

	params.ActorID = fixture.outsider.User.ID
	params.Scope = finance.ScopeMine
	if _, err := fixture.reporting.Summary(ctx, params); !errors.Is(err, finance.ErrNotFound) {
		t.Errorf("outsider error = %v, want ErrNotFound", err)
	}

	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "5", "GBP", reportDate(2026, 10, 15), false)
	params.ActorID = fixture.admin.User.ID
	params.Scope = finance.ScopeAll
	incomplete, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("incomplete Summary() error = %v", err)
	}
	if incomplete.Complete || incomplete.MissingEntryCount != 1 || len(incomplete.MissingRates) != 1 {
		t.Errorf("missing-rate summary = %+v", incomplete)
	}
	if incomplete.SpendingMinor != 122500 {
		t.Errorf("incomplete spending = %d, want unchanged 122500", incomplete.SpendingMinor)
	}
	missing := incomplete.MissingRates[0]
	if missing.FromCurrencyCode != "GBP" || missing.ToCurrencyCode != "RSD" || missing.EntryDate != "2026-10-15" {
		t.Errorf("missing rate = %+v", missing)
	}
}

func TestSummaryDateBoundariesAndRounding(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := context.Background()

	fixture.rate(t, "EUR", "RSD", "100", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "1", "EUR", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "1", "EUR", reportDate(2026, 10, 16), false)

	params := SummaryParams{
		HouseholdID:     fixture.householdID,
		ActorID:         fixture.admin.User.ID,
		Scope:           finance.ScopeAll,
		From:            reportDate(2026, 10, 1),
		To:              reportDate(2026, 10, 31),
		DisplayCurrency: "RSD",
	}
	summary, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.IncludedEntryCount != 1 || summary.MissingEntryCount != 1 || summary.SpendingMinor != 10000 {
		t.Errorf("date boundary summary = %+v", summary)
	}
	if len(summary.MissingRates) != 1 || summary.MissingRates[0].EntryDate != "2026-10-16" {
		t.Errorf("missing dates = %+v", summary.MissingRates)
	}

	// Monthly rates apply across the calendar month and stop at the boundary.
	fixture.rate(t, "USD", "RSD", "100", reportDate(2026, 10, 1), true)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "1", "USD", reportDate(2026, 10, 31), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "1", "USD", reportDate(2026, 11, 1), false)
	params.To = reportDate(2026, 11, 30)
	summary, err = fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("monthly Summary() error = %v", err)
	}
	if summary.IncomeMinor != 10000 {
		t.Errorf("monthly income = %d, want 10000", summary.IncomeMinor)
	}
	if summary.MissingEntryCount != 2 {
		t.Errorf("monthly missing count = %d, want 2", summary.MissingEntryCount)
	}

	// Rate rounding happens at display minor-unit precision, half-even.
	fixture.rate(t, "RSD", "EUR", "0.1", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "25.45", "RSD", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "25.55", "RSD", reportDate(2026, 10, 15), false)
	params.DisplayCurrency = "EUR"
	params.From = reportDate(2026, 10, 15)
	params.To = reportDate(2026, 10, 15)
	summary, err = fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("rounding Summary() error = %v", err)
	}
	if summary.IncomeMinor != 510 {
		t.Errorf("rounded income = %d, want 510 (5.10 EUR)", summary.IncomeMinor)
	}
	if summary.DisplayCurrency != "EUR" || summary.DisplayMinorUnits != 2 {
		t.Errorf("display currency = %s/%d", summary.DisplayCurrency, summary.DisplayMinorUnits)
	}
}

func TestSummaryDefaultsAndValidation(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_options SET default_display_currency_code = 'EUR'
		WHERE household_id = $1`, fixture.householdID); err != nil {
		t.Fatalf("set display default: %v", err)
	}

	params := SummaryParams{
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Scope:       finance.ScopeMine,
		From:        reportDate(2026, 10, 1),
		To:          reportDate(2026, 10, 31),
	}
	summary, err := fixture.reporting.Summary(ctx, params)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.DisplayCurrency != "EUR" || !summary.Complete || summary.EntryCount != 0 {
		t.Errorf("default summary = %+v", summary)
	}

	params.DisplayCurrency = "XXX"
	if _, err := fixture.reporting.Summary(ctx, params); err == nil {
		t.Error("invalid display currency error = nil")
	}

	params.DisplayCurrency = "RSD"
	params.From = time.Time{}
	if _, err := fixture.reporting.Summary(ctx, params); err == nil {
		t.Error("missing from date error = nil")
	}

	params.From = reportDate(2026, 10, 31)
	params.To = reportDate(2026, 10, 1)
	if _, err := fixture.reporting.Summary(ctx, params); err == nil {
		t.Error("reversed date range error = nil")
	}
}

func TestSummaryConcurrentWritesRemainConsistent(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := context.Background()
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "100", "RSD", reportDate(2026, 10, 15), false)

	params := SummaryParams{
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Scope:       finance.ScopeAll,
		From:        reportDate(2026, 10, 1),
		To:          reportDate(2026, 10, 31),
	}

	writer := func() {
		for i := range 30 {
			display := "RSD"
			if i%2 == 0 {
				display = "JPY"
			}
			if _, err := fixture.pool.Exec(ctx,
				"UPDATE household_options SET default_display_currency_code = $1 WHERE household_id = $2",
				display, fixture.householdID,
			); err != nil {
				t.Errorf("update display currency: %v", err)
				return
			}
			if _, err := fixture.pool.Exec(ctx, `
				INSERT INTO exchange_rates
					(household_id, from_currency_code, to_currency_code, rate, rate_date, is_monthly)
				VALUES ($1, 'EUR', 'RSD', $2, $3, false)
				ON CONFLICT (household_id, from_currency_code, to_currency_code, rate_date)
					WHERE NOT is_monthly
				DO UPDATE SET rate = EXCLUDED.rate, updated_at = now()`,
				fixture.householdID, "117.5", reportDate(2026, 10, 15),
			); err != nil {
				t.Errorf("upsert rate: %v", err)
				return
			}
			if _, err := fixture.finance.Create(ctx, finance.CreateParams{
				Kind:         finance.KindSpending,
				HouseholdID:  fixture.householdID,
				ActorID:      fixture.admin.User.ID,
				Amount:       "1",
				CurrencyCode: "EUR",
				EntryDate:    reportDate(2026, 10, 15),
			}); err != nil {
				t.Errorf("create concurrent entry: %v", err)
				return
			}
		}
	}

	check := func(summary Summary) {
		if summary.IncludedEntryCount+summary.MissingEntryCount != summary.EntryCount {
			t.Errorf("mixed snapshot: included %d + missing %d != entries %d",
				summary.IncludedEntryCount, summary.MissingEntryCount, summary.EntryCount)
		}
		if summary.Complete != (summary.MissingEntryCount == 0) {
			t.Errorf("complete = %v with %d missing entries", summary.Complete, summary.MissingEntryCount)
		}
		if summary.NetMinor != summary.IncomeMinor-summary.SpendingMinor {
			t.Errorf("net = %d, want %d", summary.NetMinor, summary.IncomeMinor-summary.SpendingMinor)
		}
		switch summary.DisplayCurrency {
		case "RSD":
			if summary.DisplayMinorUnits != 2 {
				t.Errorf("RSD minor units = %d, want 2", summary.DisplayMinorUnits)
			}
		case "JPY":
			if summary.DisplayMinorUnits != 0 {
				t.Errorf("JPY minor units = %d, want 0", summary.DisplayMinorUnits)
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writer()
	}()

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				summary, err := fixture.reporting.Summary(ctx, params)
				if err != nil {
					t.Errorf("Summary() error = %v", err)
					return
				}
				check(summary)
			}
		}()
	}
	wg.Wait()
}
