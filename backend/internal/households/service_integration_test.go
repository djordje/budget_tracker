//go:build integration

package households

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/testdb"
)

type testFixture struct {
	pool       *pgxpool.Pool
	service    *Service
	user       auth.Session
	otherUser  auth.Session
	household  Household
	otherHouse Household
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	pool := testdb.New(t)
	authService := auth.NewService(pool)
	service := NewService(pool)

	user := registerUser(t, authService, "ada@example.com")
	other := registerUser(t, authService, "grace@example.com")
	household, err := service.CreateAndActivate(context.Background(), user.User.ID, user.TokenHash, "  Home  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	otherHouse, err := service.CreateAndActivate(context.Background(), other.User.ID, other.TokenHash, "Other home")
	if err != nil {
		t.Fatalf("Create() other error = %v", err)
	}

	return &testFixture{
		pool:       pool,
		service:    service,
		user:       user,
		otherUser:  other,
		household:  household,
		otherHouse: otherHouse,
	}
}

func registerUser(t *testing.T, service *auth.Service, email string) auth.Session {
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

func TestCreateListAndGet(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	if fixture.household.Name != "Home" {
		t.Errorf("household name = %q, want Home", fixture.household.Name)
	}
	if !fixture.household.IsAdmin() {
		t.Errorf("creator role = %q, want admin", fixture.household.Role)
	}

	var activeHousehold *string
	if err := fixture.pool.QueryRow(ctx,
		"SELECT active_household_id::text FROM sessions WHERE token_hash = $1",
		fixture.user.TokenHash,
	).Scan(&activeHousehold); err != nil {
		t.Fatalf("query active household: %v", err)
	}
	if activeHousehold == nil || *activeHousehold != fixture.household.ID.String() {
		t.Errorf("session active household = %v, want %s", activeHousehold, fixture.household.ID)
	}

	households, err := fixture.service.List(ctx, fixture.user.User.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(households) != 1 || households[0].ID != fixture.household.ID {
		t.Errorf("List() = %+v, want only the user's household", households)
	}

	loaded, err := fixture.service.Get(ctx, fixture.user.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.ID != fixture.household.ID || !loaded.IsAdmin() {
		t.Errorf("Get() = %+v", loaded)
	}

	options, err := fixture.service.GetOptions(ctx, fixture.user.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("GetOptions() error = %v", err)
	}
	if options.DefaultIncomeCurrencyCode != "RSD" ||
		options.DefaultSpendingCurrencyCode != "RSD" ||
		options.DefaultDisplayCurrencyCode != "RSD" {
		t.Errorf("default options = %+v, want RSD", options)
	}
}

func TestCrossHouseholdAccessIsNotFound(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.Get(ctx, fixture.user.User.ID, fixture.otherHouse.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() other household error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Rename(ctx, fixture.user.User.ID, fixture.otherHouse.ID, "Hijacked"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename() other household error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.Members(ctx, fixture.user.User.ID, fixture.otherHouse.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Members() other household error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.GetOptions(ctx, fixture.user.User.ID, fixture.otherHouse.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOptions() other household error = %v, want ErrNotFound", err)
	}
}

func TestMemberCannotAdministerHousehold(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`,
		fixture.household.ID, fixture.otherUser.User.ID,
	); err != nil {
		t.Fatalf("insert member membership: %v", err)
	}

	memberHousehold, err := fixture.service.Get(ctx, fixture.otherUser.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("member Get() error = %v", err)
	}
	if memberHousehold.Role != RoleMember {
		t.Errorf("role = %q, want member", memberHousehold.Role)
	}

	if _, err := fixture.service.Rename(ctx, fixture.otherUser.User.ID, fixture.household.ID, "Renamed"); !errors.Is(err, ErrForbidden) {
		t.Errorf("member Rename() error = %v, want ErrForbidden", err)
	}
	if _, err := fixture.service.Members(ctx, fixture.otherUser.User.ID, fixture.household.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("member Members() error = %v, want ErrForbidden", err)
	}

	members, err := fixture.service.Members(ctx, fixture.user.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("admin Members() error = %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("member count = %d, want 2", len(members))
	}
	if members[0].Email == "" || members[1].Email == "" {
		t.Error("member emails are empty")
	}
}

func TestAdminCanRename(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	renamed, err := fixture.service.Rename(ctx, fixture.user.User.ID, fixture.household.ID, "  New name ")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if renamed.Name != "New name" {
		t.Errorf("name = %q, want New name", renamed.Name)
	}
}

func TestNameValidation(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	for _, name := range []string{"", "   ", strings.Repeat("x", 101), "bad\nname"} {
		_, err := fixture.service.CreateAndActivate(ctx, fixture.user.User.ID, fixture.user.TokenHash, name)
		if err == nil {
			t.Errorf("Create(%q) error = nil, want validation error", name)
			continue
		}
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("Create(%q) error = %T, want ValidationError", name, err)
		}
	}
}

func TestCreateAndActivateRollsBackOnActivationFailure(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.CreateAndActivate(ctx,
		fixture.user.User.ID, strings.Repeat("ab", 32), "Orphan",
	); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("CreateAndActivate() invalid session error = %v, want ErrSessionNotFound", err)
	}
	assertHouseholdCount(t, ctx, fixture.pool, "Orphan", 0)

	created, err := fixture.service.CreateAndActivate(ctx,
		fixture.user.User.ID, fixture.user.TokenHash, "Orphan",
	)
	if err != nil {
		t.Fatalf("CreateAndActivate() retry error = %v", err)
	}
	assertHouseholdCount(t, ctx, fixture.pool, "Orphan", 1)

	if _, err := fixture.service.CreateAndActivate(ctx,
		fixture.otherUser.User.ID, fixture.user.TokenHash, "Mismatch",
	); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("CreateAndActivate() cross-user error = %v, want ErrSessionNotFound", err)
	}
	assertHouseholdCount(t, ctx, fixture.pool, "Mismatch", 0)

	loaded, err := fixture.service.Get(ctx, fixture.user.User.ID, created.ID)
	if err != nil {
		t.Fatalf("Get() created household error = %v", err)
	}
	if !loaded.IsAdmin() {
		t.Errorf("creator role = %q, want admin", loaded.Role)
	}
}

func assertHouseholdCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM households WHERE name = $1", name).Scan(&count); err != nil {
		t.Fatalf("count households named %q: %v", name, err)
	}
	if count != want {
		t.Errorf("households named %q = %d, want %d", name, count, want)
	}
}
