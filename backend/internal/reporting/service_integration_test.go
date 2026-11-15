//go:build integration

package reporting

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "create household")
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, household.ID, member.User.ID,
	); err != nil {
		require.NoError(t, err, "add member")
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
		require.NoError(t, err, "register %s", email)
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
		require.NoError(t, err, "create %s entry", kind)
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
		require.NoError(t, err, "create exchange rate %s/%s", from, to)
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
	require.NoError(t, err, "Summary() error")
	assert.Equal(t, int64(210000), summary.IncomeMinor, "income")
	assert.Equal(t, int64(122500), summary.SpendingMinor, "spending")
	assert.Equal(t, int64(87500), summary.NetMinor, "net")
	assert.Equal(t, 4, summary.EntryCount, "summary = %+v", summary)
	assert.Equal(t, 4, summary.IncludedEntryCount, "summary = %+v", summary)
	assert.True(t, summary.Complete, "summary = %+v", summary)

	params.ActorID = fixture.admin.User.ID
	params.Scope = finance.ScopeMine
	mine, err := fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "admin mine Summary() error")
	assert.Equal(t, int64(10000), mine.IncomeMinor, "admin mine = %+v", mine)
	assert.Equal(t, int64(117500), mine.SpendingMinor, "admin mine = %+v", mine)
	assert.Equal(t, 2, mine.EntryCount, "admin mine = %+v", mine)

	params.ActorID = fixture.member.User.ID
	memberMine, err := fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "member mine Summary() error")
	assert.Equal(t, int64(200000), memberMine.IncomeMinor, "member mine = %+v", memberMine)
	assert.Equal(t, 1, memberMine.EntryCount, "member mine = %+v", memberMine)

	params.Scope = finance.ScopeAll
	_, err = fixture.reporting.Summary(ctx, params)
	assert.ErrorIs(t, err, finance.ErrForbidden, "member Summary(all)")

	params.ActorID = fixture.outsider.User.ID
	params.Scope = finance.ScopeMine
	_, err = fixture.reporting.Summary(ctx, params)
	assert.ErrorIs(t, err, finance.ErrNotFound, "outsider Summary()")

	fixture.entry(t, fixture.admin.User.ID, finance.KindSpending, "5", "GBP", reportDate(2026, 10, 15), false)
	params.ActorID = fixture.admin.User.ID
	params.Scope = finance.ScopeAll
	incomplete, err := fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "incomplete Summary() error")
	assert.False(t, incomplete.Complete, "missing-rate summary = %+v", incomplete)
	assert.Equal(t, 1, incomplete.MissingEntryCount, "missing-rate summary = %+v", incomplete)
	require.Len(t, incomplete.MissingRates, 1, "missing-rate summary = %+v", incomplete)
	assert.Equal(t, int64(122500), incomplete.SpendingMinor, "incomplete spending")
	missing := incomplete.MissingRates[0]
	assert.Equal(t, "GBP", missing.FromCurrencyCode, "missing rate = %+v", missing)
	assert.Equal(t, "RSD", missing.ToCurrencyCode, "missing rate = %+v", missing)
	assert.Equal(t, "2026-10-15", missing.EntryDate, "missing rate = %+v", missing)
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
	require.NoError(t, err, "Summary() error")
	assert.Equal(t, 1, summary.IncludedEntryCount, "date boundary summary = %+v", summary)
	assert.Equal(t, 1, summary.MissingEntryCount, "date boundary summary = %+v", summary)
	assert.Equal(t, int64(10000), summary.SpendingMinor, "date boundary summary = %+v", summary)
	require.Len(t, summary.MissingRates, 1, "missing dates")
	assert.Equal(t, "2026-10-16", summary.MissingRates[0].EntryDate, "missing dates")

	// Monthly rates apply across the calendar month and stop at the boundary.
	fixture.rate(t, "USD", "RSD", "100", reportDate(2026, 10, 1), true)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "1", "USD", reportDate(2026, 10, 31), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "1", "USD", reportDate(2026, 11, 1), false)
	params.To = reportDate(2026, 11, 30)
	summary, err = fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "monthly Summary() error")
	assert.Equal(t, int64(10000), summary.IncomeMinor, "monthly income")
	assert.Equal(t, 2, summary.MissingEntryCount, "monthly missing count")

	// Rate rounding happens at display minor-unit precision, half-even.
	fixture.rate(t, "RSD", "EUR", "0.1", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "25.45", "RSD", reportDate(2026, 10, 15), false)
	fixture.entry(t, fixture.admin.User.ID, finance.KindIncome, "25.55", "RSD", reportDate(2026, 10, 15), false)
	params.DisplayCurrency = "EUR"
	params.From = reportDate(2026, 10, 15)
	params.To = reportDate(2026, 10, 15)
	summary, err = fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "rounding Summary() error")
	assert.Equal(t, int64(510), summary.IncomeMinor, "rounded income")
	assert.Equal(t, "EUR", summary.DisplayCurrency, "display currency")
	assert.Equal(t, int32(2), summary.DisplayMinorUnits, "display minor units")
}

func TestSummaryDefaultsAndValidation(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_options SET default_display_currency_code = 'EUR'
		WHERE household_id = $1`, fixture.householdID); err != nil {
		require.NoError(t, err, "set display default")
	}

	params := SummaryParams{
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Scope:       finance.ScopeMine,
		From:        reportDate(2026, 10, 1),
		To:          reportDate(2026, 10, 31),
	}
	summary, err := fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "Summary() error")
	assert.Equal(t, "EUR", summary.DisplayCurrency, "default summary = %+v", summary)
	assert.True(t, summary.Complete, "default summary = %+v", summary)
	assert.Zero(t, summary.EntryCount, "default summary = %+v", summary)

	displayOverride := "USD"
	if _, err := households.NewService(fixture.pool).SetUserOptions(ctx, fixture.admin.User.ID, fixture.householdID, households.UpdateUserOptionsParams{
		DefaultDisplayCurrencyCode: &displayOverride,
	}); err != nil {
		require.NoError(t, err, "set display override")
	}
	summary, err = fixture.reporting.Summary(ctx, params)
	require.NoError(t, err, "Summary() with override error")
	assert.Equal(t, "USD", summary.DisplayCurrency, "override display currency")

	params.DisplayCurrency = "XXX"
	_, err = fixture.reporting.Summary(ctx, params)
	assert.Error(t, err, "invalid display currency should be rejected")

	params.DisplayCurrency = "RSD"
	params.From = time.Time{}
	_, err = fixture.reporting.Summary(ctx, params)
	assert.Error(t, err, "missing from date should be rejected")

	params.From = reportDate(2026, 10, 31)
	params.To = reportDate(2026, 10, 1)
	_, err = fixture.reporting.Summary(ctx, params)
	assert.Error(t, err, "reversed date range should be rejected")
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
				assert.NoError(t, err, "update display currency")
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
				assert.NoError(t, err, "upsert rate")
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
				assert.NoError(t, err, "create concurrent entry")
				return
			}
		}
	}

	check := func(summary Summary) {
		assert.Equal(t, summary.EntryCount, summary.IncludedEntryCount+summary.MissingEntryCount, "mixed snapshot")
		assert.Equal(t, summary.MissingEntryCount == 0, summary.Complete, "complete with %d missing entries", summary.MissingEntryCount)
		assert.Equal(t, summary.IncomeMinor-summary.SpendingMinor, summary.NetMinor, "net")
		switch summary.DisplayCurrency {
		case "RSD":
			assert.Equal(t, int32(2), summary.DisplayMinorUnits, "RSD minor units")
		case "JPY":
			assert.Zero(t, summary.DisplayMinorUnits, "JPY minor units")
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
					assert.NoError(t, err, "Summary()")
					return
				}
				check(summary)
			}
		}()
	}
	wg.Wait()
}
