//go:build integration

package finance

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
	"budgettracker/internal/testdb"
)

type financeFixture struct {
	pool        *pgxpool.Pool
	service     *Service
	householdID uuid.UUID
	admin       auth.Session
	member      auth.Session
	outsider    auth.Session
}

func newFinanceFixture(t *testing.T) *financeFixture {
	t.Helper()
	pool := testdb.New(t)
	authService := auth.NewService(pool, nil)
	householdService := households.NewService(pool)
	service := NewService(pool)
	ctx := context.Background()

	admin := registerFinanceUser(t, authService, "admin@example.com")
	member := registerFinanceUser(t, authService, "member@example.com")
	outsider := registerFinanceUser(t, authService, "outsider@example.com")

	household, err := householdService.CreateAndActivate(ctx, households.CreateParams{
		UserID:           admin.User.ID,
		SessionTokenHash: admin.TokenHash,
		Name:             "Home",
		CurrencyCode:     "RSD",
	})
	require.NoError(t, err, "create household")
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, household.ID, member.User.ID,
	); err != nil {
		require.NoError(t, err, "add member")
	}

	return &financeFixture{
		pool:        pool,
		service:     service,
		householdID: household.ID,
		admin:       admin,
		member:      member,
		outsider:    outsider,
	}
}

func registerFinanceUser(t *testing.T, service *auth.Service, email string) auth.Session {
	t.Helper()
	session, err := service.Register(context.Background(), auth.RegisterParams{
		FirstName: "Test",
		LastName:  "User",
		Email:     email,
		Password:  "correct horse battery staple",
	})
	require.NoError(t, err, "register %s", email)
	return session
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func strPtr(value string) *string { return &value }

// vanishUpdatedRow makes updates to table report no updated row by deleting the
// row in a BEFORE UPDATE trigger and returning NULL. That is the same statement
// outcome a service sees when a concurrent delete commits between its load and
// its write, without sleeps, retries, or a production-code test seam.
func vanishUpdatedRow(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION test_vanish_updated_row() RETURNS trigger AS $$
		BEGIN
			EXECUTE format('DELETE FROM %I WHERE id = $1', TG_TABLE_NAME) USING OLD.id;
			RETURN NULL;
		END
		$$ LANGUAGE plpgsql`); err != nil {
		require.NoError(t, err, "create vanish trigger function")
	}
	identifier := pgx.Identifier{table}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TRIGGER test_vanish_updated_row BEFORE UPDATE ON "+identifier+
		" FOR EACH ROW EXECUTE FUNCTION test_vanish_updated_row()"); err != nil {
		require.NoError(t, err, "create vanish trigger on %s", table)
	}
}

func TestCurrencies(t *testing.T) {
	fixture := newFinanceFixture(t)
	currencies, err := fixture.service.Currencies(context.Background())
	require.NoError(t, err, "Currencies() error")
	byCode := make(map[string]Currency, len(currencies))
	for _, currency := range currencies {
		byCode[currency.Code] = currency
	}
	assert.Equal(t, int32(2), byCode["RSD"].MinorUnits, "RSD minor units")
	assert.Equal(t, int32(0), byCode["JPY"].MinorUnits, "JPY minor units")
}

func TestCreateUsesDefaultsAndValidation(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_options
		SET default_spending_currency_code = 'EUR'
		WHERE household_id = $1`, fixture.householdID); err != nil {
		require.NoError(t, err, "set spending default")
	}

	income, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "1234.56",
		Description: "  Salary  ",
	})
	require.NoError(t, err, "Create() error")
	assert.Equal(t, "RSD", income.CurrencyCode, "default income currency")
	assert.Equal(t, int32(2), income.CurrencyMinorUnits, "default income minor units")
	assert.Equal(t, int64(123456), income.AmountMinor, "amount minor")
	require.NotNil(t, income.UserID, "user_id")
	assert.Equal(t, fixture.admin.User.ID, *income.UserID, "user_id")
	require.NotNil(t, income.Description, "description")
	assert.Equal(t, "Salary", *income.Description, "description")
	assert.Equal(t, today().Format(entryDateLayout), income.EntryDate.Format(entryDateLayout), "default date")

	spending, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindSpending,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "10.5",
		EntryDate:   date(2026, 10, 5),
	})
	require.NoError(t, err, "Create spending error")
	assert.Equal(t, "EUR", spending.CurrencyCode, "spending = %+v", spending)
	assert.Equal(t, int64(1050), spending.AmountMinor, "spending = %+v", spending)

	householdWide, err := fixture.service.Create(ctx, CreateParams{
		Kind:          KindSpending,
		HouseholdID:   fixture.householdID,
		ActorID:       fixture.admin.User.ID,
		Amount:        "25.00",
		CurrencyCode:  "EUR",
		HouseholdWide: true,
	})
	require.NoError(t, err, "Create household-wide error")
	assert.Nil(t, householdWide.UserID, "household-wide user_id")

	memberEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.member.User.ID,
		Amount:      "5",
	})
	require.NoError(t, err, "member Create() error")
	require.NotNil(t, memberEntry.UserID, "member entry attribution")
	assert.Equal(t, fixture.member.User.ID, *memberEntry.UserID, "member entry attribution")

	_, err = fixture.service.Create(ctx, CreateParams{
		Kind:          KindIncome,
		HouseholdID:   fixture.householdID,
		ActorID:       fixture.member.User.ID,
		Amount:        "5",
		HouseholdWide: true,
	})
	assert.ErrorIs(t, err, ErrForbidden, "member household-wide create")

	tests := []struct {
		name  string
		param CreateParams
		field string
	}{
		{
			name:  "invalid amount",
			param: CreateParams{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "1.234"},
			field: "amount",
		},
		{
			name:  "unknown currency",
			param: CreateParams{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "1", CurrencyCode: "XXX"},
			field: "currency_code",
		},
		{
			name:  "zero amount",
			param: CreateParams{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "0"},
			field: "amount",
		},
		{
			name:  "long description",
			param: CreateParams{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "1", Description: strings.Repeat("x", 501)},
			field: "description",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fixture.service.Create(ctx, tt.param)
			require.Error(t, err, "Create()")
			var validation *ValidationError
			require.ErrorAs(t, err, &validation, "Create()")
			assert.Contains(t, validation.Fields, tt.field, "fields = %v", validation.Fields)
		})
	}
}

func TestGetAndModifyAuthorization(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	adminEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "10", EntryDate: date(2026, 10, 1),
	})
	require.NoError(t, err, "create admin entry")
	memberEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID,
		Amount: "20", EntryDate: date(2026, 10, 2),
	})
	require.NoError(t, err, "create member entry")
	householdWide, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "30", EntryDate: date(2026, 10, 3), HouseholdWide: true,
	})
	require.NoError(t, err, "create household-wide entry")

	_, err = fixture.service.Get(ctx, fixture.admin.User.ID, KindIncome, memberEntry.ID)
	assert.NoError(t, err, "admin Get member entry")

	_, err = fixture.service.Get(ctx, fixture.member.User.ID, KindIncome, adminEntry.ID)
	assert.ErrorIs(t, err, ErrNotFound, "member Get admin entry")

	_, err = fixture.service.Get(ctx, fixture.member.User.ID, KindSpending, householdWide.ID)
	assert.ErrorIs(t, err, ErrNotFound, "member Get household-wide entry")

	_, err = fixture.service.Get(ctx, fixture.outsider.User.ID, KindIncome, adminEntry.ID)
	assert.ErrorIs(t, err, ErrNotFound, "outsider Get")

	amount := "25.50"
	updated, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	})
	require.NoError(t, err, "member update own entry error")
	assert.Equal(t, int64(2550), updated.AmountMinor, "updated amount")

	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.admin.User.ID, Amount: &amount,
	})
	assert.ErrorIs(t, err, ErrForbidden, "admin update member entry")
	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: adminEntry.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	})
	assert.ErrorIs(t, err, ErrNotFound, "member update admin entry")
	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind: KindSpending, ID: householdWide.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	})
	assert.ErrorIs(t, err, ErrNotFound, "member update household-wide entry")
	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindSpending, ID: householdWide.ID, ActorID: fixture.admin.User.ID, Amount: &amount,
	}); err != nil {
		assert.NoError(t, err, "admin update household-wide entry")
	}

	euro := "EUR"
	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, CurrencyCode: &euro,
	})
	assert.Error(t, err, "currency change without amount should be rejected")

	empty := ""
	cleared, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, Description: &empty,
	})
	require.NoError(t, err, "clear description error")
	assert.Nil(t, cleared.Description, "cleared description")

	err = fixture.service.Delete(ctx, fixture.admin.User.ID, KindIncome, memberEntry.ID)
	assert.ErrorIs(t, err, ErrForbidden, "admin delete member entry")

	err = fixture.service.Delete(ctx, fixture.member.User.ID, KindIncome, adminEntry.ID)
	assert.ErrorIs(t, err, ErrNotFound, "member delete admin entry")

	err = fixture.service.Delete(ctx, fixture.member.User.ID, KindSpending, householdWide.ID)
	assert.ErrorIs(t, err, ErrNotFound, "member delete household-wide entry")

	assert.NoError(t, fixture.service.Delete(ctx, fixture.member.User.ID, KindIncome, memberEntry.ID), "member delete own entry")
	assert.NoError(t, fixture.service.Delete(ctx, fixture.admin.User.ID, KindSpending, householdWide.ID), "admin delete household-wide entry")
}

func TestUpdateMapsVanishedRowToNotFound(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	created, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "10", EntryDate: date(2026, 10, 1),
	})
	require.NoError(t, err, "create entry")

	vanishUpdatedRow(t, fixture.pool, KindIncome.table())

	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: created.ID, ActorID: fixture.admin.User.ID, Amount: strPtr("12"),
	})
	assert.ErrorIs(t, err, ErrNotFound, "update of an entry deleted between load and write")

	var remaining int
	require.NoError(t, fixture.pool.QueryRow(ctx,
		"SELECT count(*) FROM incomes WHERE id = $1", created.ID,
	).Scan(&remaining), "count entries")
	assert.Equal(t, 0, remaining, "row deleted by the trigger is still stored")
}

// TestOversizedAmountLeavesNoPartialWrite checks that an amount outside the
// int64 minor-unit range is rejected before any statement runs: a rejected
// create inserts no row, and a rejected update applies none of its other
// changes. Both adapters pass the raw amount string through, so the service
// owns this boundary.
func TestOversizedAmountLeavesNoPartialWrite(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	oversized := strings.Repeat("9", 20)

	_, err := fixture.service.Create(ctx, CreateParams{
		Kind:         KindIncome,
		HouseholdID:  fixture.householdID,
		ActorID:      fixture.admin.User.ID,
		Amount:       oversized,
		CurrencyCode: "RSD",
		EntryDate:    date(2026, 10, 15),
	})
	require.Error(t, err, "Create() with an oversized amount")
	var validation *ValidationError
	require.ErrorAs(t, err, &validation, "Create() with an oversized amount")
	assert.Contains(t, validation.Fields, "amount", "fields = %v", validation.Fields)

	var rows int
	require.NoError(t, fixture.pool.QueryRow(ctx,
		"SELECT count(*) FROM incomes WHERE household_id = $1", fixture.householdID,
	).Scan(&rows), "count incomes")
	assert.Zero(t, rows, "rejected create inserted a row")

	created, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "10", CurrencyCode: "RSD", EntryDate: date(2026, 10, 15), Description: "original",
	})
	require.NoError(t, err, "create entry")

	otherDate := date(2026, 11, 20)
	_, err = fixture.service.Update(ctx, UpdateParams{
		Kind:        KindIncome,
		ID:          created.ID,
		ActorID:     fixture.admin.User.ID,
		Amount:      &oversized,
		EntryDate:   &otherDate,
		Description: strPtr("partially applied"),
	})
	require.Error(t, err, "Update() with an oversized amount")
	var updateValidation *ValidationError
	require.ErrorAs(t, err, &updateValidation, "Update() with an oversized amount")
	assert.Contains(t, updateValidation.Fields, "amount", "fields = %v", updateValidation.Fields)

	stored, err := fixture.service.Get(ctx, fixture.admin.User.ID, KindIncome, created.ID)
	require.NoError(t, err, "Get() after a rejected update")
	assert.Equal(t, created.AmountMinor, stored.AmountMinor, "amount after rejected update")
	assert.Equal(t, created.Description, stored.Description, "description after rejected update")
	assert.Equal(t, created.EntryDate, stored.EntryDate, "entry date after rejected update")
	assert.Equal(t, created.UpdatedAt, stored.UpdatedAt, "updated_at after rejected update")
}

func TestListScopesFiltersAndPagination(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	entries := []CreateParams{
		{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "10", EntryDate: date(2026, 10, 1)},
		{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Amount: "20", EntryDate: date(2026, 10, 2)},
		{Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "30", EntryDate: date(2026, 10, 3), HouseholdWide: true},
	}
	for _, params := range entries {
		if _, err := fixture.service.Create(ctx, params); err != nil {
			require.NoError(t, err, "create list entry")
		}
	}

	adminMine, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Scope: ScopeMine,
	})
	require.NoError(t, err, "admin List mine error")
	assert.Equal(t, 1, adminMine.Total, "admin mine = %+v", adminMine)
	assert.Len(t, adminMine.Entries, 1, "admin mine = %+v", adminMine)

	adminAll, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Scope: ScopeAll,
	})
	require.NoError(t, err, "admin List all error")
	assert.Equal(t, 3, adminAll.Total, "admin all total")
	require.Len(t, adminAll.Entries, 3, "admin all entries")
	assert.Equal(t, int64(3000), adminAll.Entries[0].AmountMinor, "newest entry amount")

	memberMine, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Scope: ScopeMine,
	})
	require.NoError(t, err, "member List mine error")
	require.Equal(t, 1, memberMine.Total, "member mine = %+v", memberMine)
	require.Len(t, memberMine.Entries, 1)
	assert.Equal(t, int64(2000), memberMine.Entries[0].AmountMinor, "member mine = %+v", memberMine)

	_, err = fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Scope: ScopeAll,
	})
	assert.ErrorIs(t, err, ErrForbidden, "member List all")
	_, err = fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.outsider.User.ID, Scope: ScopeMine,
	})
	assert.ErrorIs(t, err, ErrNotFound, "outsider List")

	from := date(2026, 10, 2)
	to := date(2026, 10, 2)
	filtered, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, From: &from, To: &to,
	})
	require.NoError(t, err, "date filter List error")
	require.Equal(t, 1, filtered.Total, "date filter = %+v", filtered)
	require.Len(t, filtered.Entries, 1)
	assert.Equal(t, int64(2000), filtered.Entries[0].AmountMinor, "date filter = %+v", filtered)

	paged, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, Page: 2, PageSize: 1,
	})
	require.NoError(t, err, "paged List error")
	assert.Equal(t, 3, paged.Total, "paged = %+v", paged)
	assert.Equal(t, 3, paged.TotalPages, "paged = %+v", paged)
	require.Len(t, paged.Entries, 1, "paged = %+v", paged)
	assert.Equal(t, int64(2000), paged.Entries[0].AmountMinor, "paged = %+v", paged)

	reversedFrom, reversedTo := date(2026, 10, 5), date(2026, 10, 1)
	_, err = fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, From: &reversedFrom, To: &reversedTo,
	})
	assert.Error(t, err, "reversed date range should be rejected")
	_, err = fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, PageSize: 101,
	})
	assert.Error(t, err, "oversized page should be rejected")
}

func TestEmptyDescriptionAndSpendingVisibility(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	entry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID,
		Amount: "9.99", CurrencyCode: "EUR", Description: "   ",
	})
	require.NoError(t, err, "Create() error")
	assert.Nil(t, entry.Description, "empty description")
	assert.Equal(t, "9.99", entry.AmountString(), "formatted amount")
	assert.Equal(t, "9.99", entry.DisplayAmount(), "display amount")

	_, err = fixture.service.Get(ctx, fixture.member.User.ID, KindSpending, entry.ID)
	assert.NoError(t, err, "member Get own spending")

	_, err = fixture.service.Get(ctx, fixture.admin.User.ID, KindSpending, entry.ID)
	assert.NoError(t, err, "admin Get member spending")

	_, err = fixture.service.Get(ctx, fixture.outsider.User.ID, KindSpending, entry.ID)
	assert.ErrorIs(t, err, ErrNotFound, "outsider Get spending")
}

func TestEntryIDsArePositiveIntegers(t *testing.T) {
	fixture := newFinanceFixture(t)
	entry, err := fixture.service.Create(context.Background(), CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "10",
	})
	require.NoError(t, err, "Create() error")
	assert.Positive(t, entry.ID, "entry ID")
}

func TestListRejectsUnrepresentableOffsets(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	boundaryPage := math.MaxInt/maxPageSize + 1
	base := ListParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Scope:       ScopeMine,
	}

	for _, page := range []int{math.MaxInt, boundaryPage + 1} {
		params := base
		params.Page = page
		params.PageSize = maxPageSize
		_, err := fixture.service.List(ctx, params)
		var validation *ValidationError
		require.ErrorAs(t, err, &validation, "List(page=%d)", page)
		assert.NotEmpty(t, validation.Fields["page"], "List(page=%d) fields = %v", page, validation.Fields)
	}

	params := base
	params.Page = boundaryPage
	params.PageSize = maxPageSize
	result, err := fixture.service.List(ctx, params)
	require.NoError(t, err, "List(boundary page) error")
	assert.Empty(t, result.Entries, "boundary page")
}

func TestDescriptionTextValidation(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	valid, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "10",
		Description: "  Line one\nLine two\tTabbed  ",
	})
	require.NoError(t, err, "Create() with control whitespace error")
	require.NotNil(t, valid.Description, "description")
	assert.Equal(t, "Line one\nLine two\tTabbed", *valid.Description, "description")

	invalid := []struct {
		name        string
		description string
	}{
		{name: "NUL", description: "bad\x00text"},
		{name: "invalid UTF-8", description: string([]byte{'b', 0xff})},
		{name: "too long", description: strings.Repeat("x", 501)},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fixture.service.Create(ctx, CreateParams{
				Kind:        KindIncome,
				HouseholdID: fixture.householdID,
				ActorID:     fixture.admin.User.ID,
				Amount:      "10",
				Description: tt.description,
			})
			var validation *ValidationError
			require.ErrorAs(t, err, &validation, "Create()")
			assert.NotEmpty(t, validation.Fields["description"], "fields = %v", validation.Fields)
		})
	}
}

func TestUserCurrencyOverridesAffectEntryDefaults(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	householdService := households.NewService(fixture.pool)

	income := "USD"
	spending := "EUR"
	if _, err := householdService.SetUserOptions(ctx, fixture.member.User.ID, fixture.householdID, households.UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode:   &income,
		DefaultSpendingCurrencyCode: &spending,
	}); err != nil {
		require.NoError(t, err, "SetUserOptions()")
	}

	incomeEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Amount: "5",
	})
	require.NoError(t, err, "member income Create() error")
	assert.Equal(t, "USD", incomeEntry.CurrencyCode, "member income default")
	spendingEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Amount: "5",
	})
	require.NoError(t, err, "member spending Create() error")
	assert.Equal(t, "EUR", spendingEntry.CurrencyCode, "member spending default")

	adminEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "5",
	})
	require.NoError(t, err, "admin income Create() error")
	assert.Equal(t, "RSD", adminEntry.CurrencyCode, "admin income default")
}
