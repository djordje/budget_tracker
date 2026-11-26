//go:build integration

package store_test

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"

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
var entrySchemaConstraints = []string{
	"incomes_membership_fk",
	"spendings_membership_fk",
	"users_first_name_length",
	"users_last_name_length",
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
// against a schema migrated only through 0008 and seeded with legacy rows that
// violate the new constraints. The migration adds each constraint NOT VALID and
// then validates it, so the failure happens during validation and the runner's
// implicit transaction must roll back the constraints it already added.
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
			// Start from a schema migrated through 0008, so the later constraints
			// do not exist and the seeded rows are accepted. Migrating then runs
			// 0009 from the same bytes as production, letting the runner verify
			// the recorded checksums.
			pool := testdb.NewWithMigrations(t, migrationsThrough(t, 8))
			ctx := context.Background()

			tc.seed(t, pool)

			applied, err := store.Migrate(ctx, pool, migrationsThrough(t, 9), "migrations")
			require.Error(t, err, "Migrate() with legacy rows that violate 0009")
			assert.Empty(t, applied, "migrations applied before the failure")
			assert.ErrorContains(t, err, "apply migration 0009_entry_membership_and_name_limits")

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, "Migrate() error")
			assert.Equal(t, tc.wantCode, pgErr.Code, "SQLSTATE")
			assert.Equal(t, tc.wantConstraint, pgErr.ConstraintName, "constraint that failed validation")

			for _, constraint := range entrySchemaConstraints {
				assert.False(t, constraintExists(t, pool, constraint), "constraint %s survived the rollback", constraint)
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

// migrationsThrough returns a view of the embedded migrations up to and
// including version. It keeps each migration's embedded SQL under its
// canonical filename, so the runner records the same checksums as a real
// deployment and a replay still fails if the SQL changes.
func migrationsThrough(t *testing.T, version int64) fs.FS {
	t.Helper()

	migrations, err := store.LoadMigrations(dbmigrations.Migrations, "migrations")
	require.NoError(t, err, "load embedded migrations")

	bounded := make(fstest.MapFS, len(migrations))
	for _, migration := range migrations {
		if migration.Version > version {
			continue
		}
		bounded[path.Join("migrations", migration.Label()+".sql")] = &fstest.MapFile{
			Data: []byte(migration.SQL),
		}
	}

	return bounded
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
