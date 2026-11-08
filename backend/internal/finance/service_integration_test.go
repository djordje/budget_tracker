//go:build integration

package finance

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
	authService := auth.NewService(pool)
	householdService := households.NewService(pool)
	service := NewService(pool)
	ctx := context.Background()

	admin := registerFinanceUser(t, authService, "admin@example.com")
	member := registerFinanceUser(t, authService, "member@example.com")
	outsider := registerFinanceUser(t, authService, "outsider@example.com")

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
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return session
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func strPtr(value string) *string { return &value }

func TestCurrencies(t *testing.T) {
	fixture := newFinanceFixture(t)
	currencies, err := fixture.service.Currencies(context.Background())
	if err != nil {
		t.Fatalf("Currencies() error = %v", err)
	}
	byCode := make(map[string]Currency, len(currencies))
	for _, currency := range currencies {
		byCode[currency.Code] = currency
	}
	if byCode["RSD"].MinorUnits != 2 {
		t.Errorf("RSD minor units = %d", byCode["RSD"].MinorUnits)
	}
	if byCode["JPY"].MinorUnits != 0 {
		t.Errorf("JPY minor units = %d", byCode["JPY"].MinorUnits)
	}
}

func TestCreateUsesDefaultsAndValidation(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_options
		SET default_spending_currency_code = 'EUR'
		WHERE household_id = $1`, fixture.householdID); err != nil {
		t.Fatalf("set spending default: %v", err)
	}

	income, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "1234.56",
		Description: "  Salary  ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if income.CurrencyCode != "RSD" || income.CurrencyMinorUnits != 2 {
		t.Errorf("default income currency = %q (%d)", income.CurrencyCode, income.CurrencyMinorUnits)
	}
	if income.AmountMinor != 123456 {
		t.Errorf("amount minor = %d", income.AmountMinor)
	}
	if income.UserID == nil || *income.UserID != fixture.admin.User.ID {
		t.Errorf("user_id = %v, want admin", income.UserID)
	}
	if income.Description == nil || *income.Description != "Salary" {
		t.Errorf("description = %v", income.Description)
	}
	if income.EntryDate.Format(entryDateLayout) != today().Format(entryDateLayout) {
		t.Errorf("default date = %v", income.EntryDate)
	}

	spending, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindSpending,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "10.5",
		EntryDate:   date(2026, 10, 5),
	})
	if err != nil {
		t.Fatalf("Create spending error = %v", err)
	}
	if spending.CurrencyCode != "EUR" || spending.AmountMinor != 1050 {
		t.Errorf("spending = %+v", spending)
	}

	householdWide, err := fixture.service.Create(ctx, CreateParams{
		Kind:          KindSpending,
		HouseholdID:   fixture.householdID,
		ActorID:       fixture.admin.User.ID,
		Amount:        "25.00",
		CurrencyCode:  "EUR",
		HouseholdWide: true,
	})
	if err != nil {
		t.Fatalf("Create household-wide error = %v", err)
	}
	if householdWide.UserID != nil {
		t.Errorf("household-wide user_id = %v, want nil", householdWide.UserID)
	}

	memberEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.member.User.ID,
		Amount:      "5",
	})
	if err != nil {
		t.Fatalf("member Create() error = %v", err)
	}
	if memberEntry.UserID == nil || *memberEntry.UserID != fixture.member.User.ID {
		t.Errorf("member entry attribution = %v", memberEntry.UserID)
	}

	if _, err := fixture.service.Create(ctx, CreateParams{
		Kind:          KindIncome,
		HouseholdID:   fixture.householdID,
		ActorID:       fixture.member.User.ID,
		Amount:        "5",
		HouseholdWide: true,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member household-wide error = %v, want ErrForbidden", err)
	}

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
			if err == nil {
				t.Fatal("Create() error = nil, want validation error")
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

func TestGetAndModifyAuthorization(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	adminEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "10", EntryDate: date(2026, 10, 1),
	})
	if err != nil {
		t.Fatalf("create admin entry: %v", err)
	}
	memberEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID,
		Amount: "20", EntryDate: date(2026, 10, 2),
	})
	if err != nil {
		t.Fatalf("create member entry: %v", err)
	}
	householdWide, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Amount: "30", EntryDate: date(2026, 10, 3), HouseholdWide: true,
	})
	if err != nil {
		t.Fatalf("create household-wide entry: %v", err)
	}

	if _, err := fixture.service.Get(ctx, fixture.admin.User.ID, KindIncome, memberEntry.ID); err != nil {
		t.Errorf("admin Get member entry error = %v", err)
	}
	if _, err := fixture.service.Get(ctx, fixture.member.User.ID, KindIncome, adminEntry.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("member Get admin entry error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Get(ctx, fixture.member.User.ID, KindSpending, householdWide.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("member Get household-wide error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Get(ctx, fixture.outsider.User.ID, KindIncome, adminEntry.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider Get error = %v, want ErrNotFound", err)
	}

	amount := "25.50"
	updated, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	})
	if err != nil {
		t.Fatalf("member update own entry error = %v", err)
	}
	if updated.AmountMinor != 2550 {
		t.Errorf("updated amount = %d", updated.AmountMinor)
	}

	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.admin.User.ID, Amount: &amount,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin update member entry error = %v, want ErrForbidden", err)
	}
	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: adminEntry.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("member update admin entry error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindSpending, ID: householdWide.ID, ActorID: fixture.member.User.ID, Amount: &amount,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("member update household-wide error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindSpending, ID: householdWide.ID, ActorID: fixture.admin.User.ID, Amount: &amount,
	}); err != nil {
		t.Errorf("admin update household-wide error = %v", err)
	}

	euro := "EUR"
	if _, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, CurrencyCode: &euro,
	}); err == nil {
		t.Error("currency change without amount error = nil, want validation error")
	}

	empty := ""
	cleared, err := fixture.service.Update(ctx, UpdateParams{
		Kind: KindIncome, ID: memberEntry.ID, ActorID: fixture.member.User.ID, Description: &empty,
	})
	if err != nil {
		t.Fatalf("clear description error = %v", err)
	}
	if cleared.Description != nil {
		t.Errorf("cleared description = %v", cleared.Description)
	}

	if err := fixture.service.Delete(ctx, fixture.admin.User.ID, KindIncome, memberEntry.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin delete member entry error = %v, want ErrForbidden", err)
	}
	if err := fixture.service.Delete(ctx, fixture.member.User.ID, KindIncome, adminEntry.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("member delete admin entry error = %v, want ErrNotFound", err)
	}
	if err := fixture.service.Delete(ctx, fixture.member.User.ID, KindSpending, householdWide.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("member delete household-wide error = %v, want ErrNotFound", err)
	}
	if err := fixture.service.Delete(ctx, fixture.member.User.ID, KindIncome, memberEntry.ID); err != nil {
		t.Errorf("member delete own entry error = %v", err)
	}
	if err := fixture.service.Delete(ctx, fixture.admin.User.ID, KindSpending, householdWide.ID); err != nil {
		t.Errorf("admin delete household-wide error = %v", err)
	}
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
			t.Fatalf("create list entry: %v", err)
		}
	}

	adminMine, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Scope: ScopeMine,
	})
	if err != nil {
		t.Fatalf("admin List mine error = %v", err)
	}
	if adminMine.Total != 1 || len(adminMine.Entries) != 1 {
		t.Errorf("admin mine = %+v, want one personal entry", adminMine)
	}

	adminAll, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Scope: ScopeAll,
	})
	if err != nil {
		t.Fatalf("admin List all error = %v", err)
	}
	if adminAll.Total != 3 || len(adminAll.Entries) != 3 {
		t.Errorf("admin all total = %d, entries = %d", adminAll.Total, len(adminAll.Entries))
	}
	if adminAll.Entries[0].AmountMinor != 3000 {
		t.Errorf("first entry amount = %d, want newest 3000", adminAll.Entries[0].AmountMinor)
	}

	memberMine, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Scope: ScopeMine,
	})
	if err != nil {
		t.Fatalf("member List mine error = %v", err)
	}
	if memberMine.Total != 1 || memberMine.Entries[0].AmountMinor != 2000 {
		t.Errorf("member mine = %+v", memberMine)
	}

	if _, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Scope: ScopeAll,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member List all error = %v, want ErrForbidden", err)
	}
	if _, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.outsider.User.ID, Scope: ScopeMine,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider List error = %v, want ErrNotFound", err)
	}

	from := date(2026, 10, 2)
	to := date(2026, 10, 2)
	filtered, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, From: &from, To: &to,
	})
	if err != nil {
		t.Fatalf("date filter List error = %v", err)
	}
	if filtered.Total != 1 || filtered.Entries[0].AmountMinor != 2000 {
		t.Errorf("date filter = %+v", filtered)
	}

	paged, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, Page: 2, PageSize: 1,
	})
	if err != nil {
		t.Fatalf("paged List error = %v", err)
	}
	if paged.Total != 3 || paged.TotalPages != 3 || len(paged.Entries) != 1 || paged.Entries[0].AmountMinor != 2000 {
		t.Errorf("paged = %+v", paged)
	}

	reversedFrom, reversedTo := date(2026, 10, 5), date(2026, 10, 1)
	if _, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, From: &reversedFrom, To: &reversedTo,
	}); err == nil {
		t.Error("reversed date range error = nil, want validation error")
	}
	if _, err := fixture.service.List(ctx, ListParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID,
		Scope: ScopeAll, PageSize: 101,
	}); err == nil {
		t.Error("oversized page error = nil, want validation error")
	}
}

func TestEmptyDescriptionAndSpendingVisibility(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()

	entry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID,
		Amount: "9.99", CurrencyCode: "EUR", Description: "   ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if entry.Description != nil {
		t.Errorf("empty description = %v, want nil", entry.Description)
	}
	if entry.AmountString() != "9.99" || entry.DisplayAmount() != "9.99" {
		t.Errorf("formatted amounts = %q, %q", entry.AmountString(), entry.DisplayAmount())
	}
	if _, err := fixture.service.Get(ctx, fixture.member.User.ID, KindSpending, entry.ID); err != nil {
		t.Errorf("member Get own spending error = %v", err)
	}
	if _, err := fixture.service.Get(ctx, fixture.admin.User.ID, KindSpending, entry.ID); err != nil {
		t.Errorf("admin Get member spending error = %v", err)
	}
	if _, err := fixture.service.Get(ctx, fixture.outsider.User.ID, KindSpending, entry.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider Get spending error = %v, want ErrNotFound", err)
	}
}

func TestEntryIDsArePositiveIntegers(t *testing.T) {
	fixture := newFinanceFixture(t)
	entry, err := fixture.service.Create(context.Background(), CreateParams{
		Kind:        KindIncome,
		HouseholdID: fixture.householdID,
		ActorID:     fixture.admin.User.ID,
		Amount:      "10",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if entry.ID <= 0 {
		t.Errorf("entry ID = %d, want a positive integer", entry.ID)
	}
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
		if !errors.As(err, &validation) {
			t.Fatalf("List(page=%d) error = %v, want ValidationError", page, err)
		}
		if validation.Fields["page"] == "" {
			t.Errorf("List(page=%d) fields = %v, want page error", page, validation.Fields)
		}
	}

	params := base
	params.Page = boundaryPage
	params.PageSize = maxPageSize
	result, err := fixture.service.List(ctx, params)
	if err != nil {
		t.Fatalf("List(boundary page) error = %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("boundary page returned %d entries, want an empty page", len(result.Entries))
	}
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
	if err != nil {
		t.Fatalf("Create() with control whitespace error = %v", err)
	}
	if valid.Description == nil || *valid.Description != "Line one\nLine two\tTabbed" {
		t.Errorf("description = %v, want trimmed newline/tab content", valid.Description)
	}

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
			if !errors.As(err, &validation) {
				t.Fatalf("Create() error = %v, want ValidationError", err)
			}
			if validation.Fields["description"] == "" {
				t.Errorf("fields = %v, want description error", validation.Fields)
			}
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
		t.Fatalf("SetUserOptions() error = %v", err)
	}

	incomeEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Amount: "5",
	})
	if err != nil {
		t.Fatalf("member income Create() error = %v", err)
	}
	if incomeEntry.CurrencyCode != "USD" {
		t.Errorf("member income default = %q, want USD", incomeEntry.CurrencyCode)
	}
	spendingEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindSpending, HouseholdID: fixture.householdID, ActorID: fixture.member.User.ID, Amount: "5",
	})
	if err != nil {
		t.Fatalf("member spending Create() error = %v", err)
	}
	if spendingEntry.CurrencyCode != "EUR" {
		t.Errorf("member spending default = %q, want EUR", spendingEntry.CurrencyCode)
	}

	adminEntry, err := fixture.service.Create(ctx, CreateParams{
		Kind: KindIncome, HouseholdID: fixture.householdID, ActorID: fixture.admin.User.ID, Amount: "5",
	})
	if err != nil {
		t.Fatalf("admin income Create() error = %v", err)
	}
	if adminEntry.CurrencyCode != "RSD" {
		t.Errorf("admin income default = %q, want household RSD", adminEntry.CurrencyCode)
	}
}
