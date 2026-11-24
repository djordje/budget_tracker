//go:build integration

package households

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	authService := auth.NewService(pool, nil)
	service := NewService(pool)

	user := registerUser(t, authService, "ada@example.com")
	other := registerUser(t, authService, "grace@example.com")
	household, err := service.CreateAndActivate(context.Background(), CreateParams{
		UserID:           user.User.ID,
		SessionTokenHash: user.TokenHash,
		Name:             "  Home  ",
		CurrencyCode:     "RSD",
	})
	require.NoError(t, err, "Create() error")
	otherHouse, err := service.CreateAndActivate(context.Background(), CreateParams{
		UserID:           other.User.ID,
		SessionTokenHash: other.TokenHash,
		Name:             "Other home",
		CurrencyCode:     "RSD",
	})
	require.NoError(t, err, "Create() other error")

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
	require.NoError(t, err, "register %s", email)
	return session
}

func TestCreateListAndGet(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	assert.Equal(t, "Home", fixture.household.Name, "household name")
	assert.True(t, fixture.household.IsAdmin(), "creator role = %q, want admin", fixture.household.Role)

	var activeHousehold *string
	if err := fixture.pool.QueryRow(ctx,
		"SELECT active_household_id::text FROM sessions WHERE token_hash = $1",
		fixture.user.TokenHash,
	).Scan(&activeHousehold); err != nil {
		require.NoError(t, err, "query active household")
	}
	require.NotNil(t, activeHousehold, "session active household")
	assert.Equal(t, fixture.household.ID.String(), *activeHousehold, "session active household")

	households, err := fixture.service.List(ctx, fixture.user.User.ID)
	require.NoError(t, err, "List() error")
	require.Len(t, households, 1, "List() = %+v", households)
	assert.Equal(t, fixture.household.ID, households[0].ID, "List() = %+v", households)

	loaded, err := fixture.service.Get(ctx, fixture.user.User.ID, fixture.household.ID)
	require.NoError(t, err, "Get() error")
	assert.Equal(t, fixture.household.ID, loaded.ID, "Get() = %+v", loaded)
	assert.True(t, loaded.IsAdmin(), "Get() = %+v", loaded)

	options, err := fixture.service.GetOptions(ctx, fixture.user.User.ID, fixture.household.ID)
	require.NoError(t, err, "GetOptions() error")
	assert.Equal(t, "RSD", options.DefaultIncomeCurrencyCode, "default options = %+v", options)
	assert.Equal(t, "RSD", options.DefaultSpendingCurrencyCode, "default options = %+v", options)
	assert.Equal(t, "RSD", options.DefaultDisplayCurrencyCode, "default options = %+v", options)
}

func TestCreateAndActivateSeedsChosenCurrency(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	created, err := fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.user.User.ID,
		SessionTokenHash: fixture.user.TokenHash,
		Name:             "Euro home",
		CurrencyCode:     " eur ",
	})
	require.NoError(t, err, "CreateAndActivate() error")

	options, err := fixture.service.GetOptions(ctx, fixture.user.User.ID, created.ID)
	require.NoError(t, err, "GetOptions() error")
	assert.Equal(t, "EUR", options.DefaultIncomeCurrencyCode, "default options = %+v", options)
	assert.Equal(t, "EUR", options.DefaultSpendingCurrencyCode, "default options = %+v", options)
	assert.Equal(t, "EUR", options.DefaultDisplayCurrencyCode, "default options = %+v", options)
}

func TestCreateAndActivateDefaultsOmittedCurrency(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	created, err := fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.user.User.ID,
		SessionTokenHash: fixture.user.TokenHash,
		Name:             "Default home",
	})
	require.NoError(t, err, "CreateAndActivate() error")

	options, err := fixture.service.GetOptions(ctx, fixture.user.User.ID, created.ID)
	require.NoError(t, err, "GetOptions() error")
	assert.Equal(t, DefaultCurrencyCode, options.DefaultIncomeCurrencyCode, "default options = %+v", options)
	assert.Equal(t, DefaultCurrencyCode, options.DefaultSpendingCurrencyCode, "default options = %+v", options)
	assert.Equal(t, DefaultCurrencyCode, options.DefaultDisplayCurrencyCode, "default options = %+v", options)
}

func TestCreateAndActivateRejectsUnknownCurrency(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	_, err := fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.user.User.ID,
		SessionTokenHash: fixture.user.TokenHash,
		Name:             "Bad currency",
		CurrencyCode:     "XXX",
	})
	var validation *ValidationError
	require.ErrorAs(t, err, &validation, "CreateAndActivate() error")
	assert.Equal(t, "currency_code", validation.Field, "validation field")
	assert.Equal(t, "Choose a supported currency.", validation.Message, "validation message")
	assertHouseholdCount(t, ctx, fixture.pool, "Bad currency", 0)
}

func TestCrossHouseholdAccessIsNotFound(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	_, err := fixture.service.Get(ctx, fixture.user.User.ID, fixture.otherHouse.ID)
	assert.ErrorIs(t, err, ErrNotFound, "Get() other household")

	_, err = fixture.service.Rename(ctx, fixture.user.User.ID, fixture.otherHouse.ID, "Hijacked")
	assert.ErrorIs(t, err, ErrNotFound, "Rename() other household")

	_, err = fixture.service.Members(ctx, fixture.user.User.ID, fixture.otherHouse.ID)
	assert.ErrorIs(t, err, ErrNotFound, "Members() other household")

	_, err = fixture.service.GetOptions(ctx, fixture.user.User.ID, fixture.otherHouse.ID)
	assert.ErrorIs(t, err, ErrNotFound, "GetOptions() other household")
}

func TestMemberCannotAdministerHousehold(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`,
		fixture.household.ID, fixture.otherUser.User.ID,
	); err != nil {
		require.NoError(t, err, "insert member membership")
	}

	memberHousehold, err := fixture.service.Get(ctx, fixture.otherUser.User.ID, fixture.household.ID)
	require.NoError(t, err, "member Get() error")
	assert.Equal(t, RoleMember, memberHousehold.Role, "role")

	_, err = fixture.service.Rename(ctx, fixture.otherUser.User.ID, fixture.household.ID, "Renamed")
	assert.ErrorIs(t, err, ErrForbidden, "member Rename()")

	_, err = fixture.service.Members(ctx, fixture.otherUser.User.ID, fixture.household.ID)
	assert.ErrorIs(t, err, ErrForbidden, "member Members()")

	members, err := fixture.service.Members(ctx, fixture.user.User.ID, fixture.household.ID)
	require.NoError(t, err, "admin Members() error")
	require.Len(t, members, 2, "member count")
	assert.NotEmpty(t, members[0].Email, "member emails")
	assert.NotEmpty(t, members[1].Email, "member emails")
}

func TestAdminCanRename(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	renamed, err := fixture.service.Rename(ctx, fixture.user.User.ID, fixture.household.ID, "  New name ")
	require.NoError(t, err, "Rename() error")
	assert.Equal(t, "New name", renamed.Name, "name")
}

func TestNameValidation(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	for _, name := range []string{"", "   ", strings.Repeat("x", 101), "bad\nname"} {
		_, err := fixture.service.CreateAndActivate(ctx, CreateParams{
			UserID:           fixture.user.User.ID,
			SessionTokenHash: fixture.user.TokenHash,
			Name:             name,
			CurrencyCode:     "RSD",
		})
		require.Error(t, err, "Create(%q)", name)
		var validation *ValidationError
		assert.ErrorAs(t, err, &validation, "Create(%q)", name)
	}
}

func TestCreateAndActivateRollsBackOnActivationFailure(t *testing.T) {
	fixture := newFixture(t)
	ctx := context.Background()

	_, err := fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.user.User.ID,
		SessionTokenHash: strings.Repeat("ab", 32),
		Name:             "Orphan",
		CurrencyCode:     "RSD",
	})
	require.ErrorIs(t, err, auth.ErrSessionNotFound, "CreateAndActivate() invalid session")
	assertHouseholdCount(t, ctx, fixture.pool, "Orphan", 0)

	created, err := fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.user.User.ID,
		SessionTokenHash: fixture.user.TokenHash,
		Name:             "Orphan",
		CurrencyCode:     "RSD",
	})
	require.NoError(t, err, "CreateAndActivate() retry error")
	assertHouseholdCount(t, ctx, fixture.pool, "Orphan", 1)

	_, err = fixture.service.CreateAndActivate(ctx, CreateParams{
		UserID:           fixture.otherUser.User.ID,
		SessionTokenHash: fixture.user.TokenHash,
		Name:             "Mismatch",
		CurrencyCode:     "RSD",
	})
	assert.ErrorIs(t, err, auth.ErrSessionNotFound, "CreateAndActivate() cross-user")
	assertHouseholdCount(t, ctx, fixture.pool, "Mismatch", 0)

	loaded, err := fixture.service.Get(ctx, fixture.user.User.ID, created.ID)
	require.NoError(t, err, "Get() created household error")
	assert.True(t, loaded.IsAdmin(), "creator role = %q, want admin", loaded.Role)
}

func assertHouseholdCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM households WHERE name = $1", name).Scan(&count); err != nil {
		require.NoError(t, err, "count households named %q", name)
	}
	assert.Equal(t, want, count, "households named %q", name)
}
