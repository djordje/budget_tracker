//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbmigrations "budgettracker/db"
	"budgettracker/internal/store"
	"budgettracker/internal/testdb"
)

// entrySchemaConstraints are the constraints added by
// 0009_entry_membership_and_name_limits.sql.
var entrySchemaConstraints = []struct {
	name  string
	table string
}{
	{name: "incomes_membership_fk", table: "incomes"},
	{name: "spendings_membership_fk", table: "spendings"},
	{name: "users_first_name_length", table: "users"},
	{name: "users_last_name_length", table: "users"},
}

func TestEntryMembershipConstraint(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	householdID := insertHousehold(t, pool, "Home")
	memberID := insertUser(t, pool, "Member", "User")
	insertMembership(t, pool, householdID, memberID)
	outsiderID := insertUser(t, pool, "Out", "Sider")

	for _, table := range []string{"incomes", "spendings"} {
		t.Run(table, func(t *testing.T) {
			require.NoError(t, insertEntry(pool, table, householdID, &memberID), "member entry")
			require.NoError(t, insertEntry(pool, table, householdID, nil), "household-wide entry")

			err := insertEntry(pool, table, householdID, &outsiderID)
			require.Error(t, err, "non-member entry")
			assert.ErrorContains(t, err, table+"_membership_fk")
			assert.ErrorContains(t, err, "violates foreign key constraint")

			var entries int
			require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&entries),
				"count %s", table)
			assert.Equal(t, 2, entries, "%s entries after the rejected insert", table)
		})
	}
}

func TestUserNameLengthConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	cases := []struct {
		name      string
		firstName string
		lastName  string
		violation string
	}{
		{name: "first name at limit", firstName: strings.Repeat("ž", 80), lastName: "User"},
		{name: "first name over limit", firstName: strings.Repeat("ž", 81), lastName: "User", violation: "users_first_name_length"},
		{name: "last name at limit", firstName: "User", lastName: strings.Repeat("a", 80)},
		{name: "last name over limit", firstName: "User", lastName: strings.Repeat("a", 81), violation: "users_last_name_length"},
		{name: "first name padding trimmed", firstName: strings.Repeat("a", 80) + "  ", lastName: "User"},
		{name: "last name padding trimmed", firstName: "User", lastName: "  " + strings.Repeat("a", 80)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, "INSERT INTO users (first_name, last_name) VALUES ($1, $2)",
				tc.firstName, tc.lastName)
			if tc.violation == "" {
				assert.NoError(t, err, "insert user")
				return
			}
			require.Error(t, err, "insert user")
			assert.ErrorContains(t, err, tc.violation)
		})
	}
}

// TestMigration0009RollsBackInvalidLegacyRows replays the delivered migration
// against a schema that has the constraints removed and legacy rows that
// violate them. The migration adds each constraint NOT VALID and then validates
// it, so the failure happens during validation and the runner's implicit
// transaction must roll back the constraints it already added.
func TestMigration0009RollsBackInvalidLegacyRows(t *testing.T) {
	cases := []struct {
		name           string
		seed           func(t *testing.T, pool *pgxpool.Pool)
		wantCountQuery string
		wantCount      int
		wantCode       string
		wantConstraint string
	}{
		{
			name:           "non-member income",
			seed:           seedNonMemberIncome,
			wantCountQuery: "SELECT count(*) FROM incomes",
			wantCount:      1,
			wantCode:       "23503",
			wantConstraint: "incomes_membership_fk",
		},
		{
			name:           "over-long first name",
			seed:           seedOverlongFirstName,
			wantCountQuery: "SELECT count(*) FROM users WHERE char_length(first_name) > 80",
			wantCount:      1,
			wantCode:       "23514",
			wantConstraint: "users_first_name_length",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := testdb.New(t)
			ctx := context.Background()

			dropEntrySchemaConstraints(t, pool)
			if _, err := pool.Exec(ctx, "DELETE FROM schema_migrations WHERE version = 9"); err != nil {
				require.NoError(t, err, "forget migration 0009")
			}
			tc.seed(t, pool)

			applied, err := store.Migrate(ctx, pool, dbmigrations.Migrations, "migrations")
			require.Error(t, err, "Migrate() with legacy rows that violate 0009")
			assert.Empty(t, applied, "migrations applied before the failure")
			assert.ErrorContains(t, err, "apply migration 0009_entry_membership_and_name_limits")

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, "Migrate() error")
			assert.Equal(t, tc.wantCode, pgErr.Code, "SQLSTATE")
			assert.Equal(t, tc.wantConstraint, pgErr.ConstraintName, "constraint that failed validation")

			for _, constraint := range entrySchemaConstraints {
				assert.False(t, constraintExists(t, pool, constraint.name), "constraint %s survived the rollback", constraint.name)
			}

			var rows int
			require.NoError(t, pool.QueryRow(ctx, tc.wantCountQuery).Scan(&rows), "count invalid rows")
			assert.Equal(t, tc.wantCount, rows, "invalid rows after the failed migration")

			var recorded int
			require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE version = 9").Scan(&recorded),
				"query schema_migrations")
			assert.Equal(t, 0, recorded, "failed migration recorded in schema_migrations")
		})
	}
}

func seedNonMemberIncome(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	householdID := insertHousehold(t, pool, "Home")
	memberID := insertUser(t, pool, "Member", "User")
	insertMembership(t, pool, householdID, memberID)
	outsiderID := insertUser(t, pool, "Out", "Sider")
	require.NoError(t, insertEntry(pool, "incomes", householdID, &outsiderID), "insert non-member income")
}

func seedOverlongFirstName(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		"INSERT INTO users (first_name, last_name) VALUES ($1, 'User')", strings.Repeat("a", 81),
	); err != nil {
		require.NoError(t, err, "insert over-long first name")
	}
}

func dropEntrySchemaConstraints(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, constraint := range entrySchemaConstraints {
		_, err := pool.Exec(context.Background(),
			"ALTER TABLE "+pgx.Identifier{constraint.table}.Sanitize()+
				" DROP CONSTRAINT "+pgx.Identifier{constraint.name}.Sanitize())
		require.NoError(t, err, "drop constraint %s", constraint.name)
	}
}

func constraintExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = $1 AND connamespace = current_schema()::regnamespace
		)`, name,
	).Scan(&exists); err != nil {
		require.NoError(t, err, "check constraint %s", name)
	}
	return exists
}

func insertUser(t *testing.T, pool *pgxpool.Pool, firstName, lastName string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO users (first_name, last_name) VALUES ($1, $2) RETURNING id", firstName, lastName,
	).Scan(&id); err != nil {
		require.NoError(t, err, "insert user %s %s", firstName, lastName)
	}
	return id
}

func insertHousehold(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO households (name) VALUES ($1) RETURNING id", name,
	).Scan(&id); err != nil {
		require.NoError(t, err, "insert household")
	}
	return id
}

func insertMembership(t *testing.T, pool *pgxpool.Pool, householdID, userID uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		"INSERT INTO household_memberships (household_id, user_id, role) VALUES ($1, $2, 'admin')",
		householdID, userID,
	); err != nil {
		require.NoError(t, err, "insert membership")
	}
}

// insertEntry inserts an entry with a fixed amount and date. A nil userID
// inserts a household-wide entry.
func insertEntry(pool *pgxpool.Pool, table string, householdID uuid.UUID, userID *uuid.UUID) error {
	_, err := pool.Exec(context.Background(),
		"INSERT INTO "+pgx.Identifier{table}.Sanitize()+
			" (household_id, user_id, amount_minor, currency_code, entry_date) VALUES ($1, $2, 1000, 'RSD', '2026-01-01')",
		householdID, userID)
	return err
}
