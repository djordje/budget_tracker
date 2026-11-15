//go:build integration

package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, integrationDatabaseURL(t))
	require.NoError(t, err, "Open() error")
	defer pool.Close()

	var value int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil {
		require.NoError(t, err, "query database")
	}
	assert.Equal(t, 1, value, "SELECT 1")
}

func TestMigrateAppliesAndTracksMigrations(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	applied, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations")
	require.NoError(t, err, "Migrate() error")
	require.Len(t, applied, 2, "applied migrations")

	if _, err := pool.Exec(ctx, "INSERT INTO things (id, name) VALUES (1, 'first')"); err != nil {
		require.NoError(t, err, "insert migrated row")
	}
	var name string
	if err := pool.QueryRow(ctx, "SELECT name FROM things WHERE id = 1").Scan(&name); err != nil {
		require.NoError(t, err, "query migrated row")
	}
	assert.Equal(t, "first", name, "name")

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		require.NoError(t, err, "query schema_migrations")
	}
	assert.Equal(t, 2, recorded, "schema_migrations count")

	records, err := AppliedMigrations(ctx, pool)
	require.NoError(t, err, "AppliedMigrations() error")
	require.Len(t, records, 2, "applied migration records")
	assert.Equal(t, int64(1), records[0].Version, "AppliedMigrations() versions")
	assert.Equal(t, int64(2), records[1].Version, "AppliedMigrations() versions")
	assert.False(t, records[0].AppliedAt.IsZero(), "AppliedMigrations() applied time")

	applied, err = Migrate(ctx, pool, integrationMigrationFixture(), "migrations")
	require.NoError(t, err, "second Migrate() error")
	assert.Empty(t, applied, "second Migrate() applied migrations")
}

func TestMigrateRejectsChangedMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	if _, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations"); err != nil {
		require.NoError(t, err, "Migrate()")
	}

	changed := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY, changed boolean);\n"),
		},
		"migrations/0002_add_name.sql": {
			Data: []byte("ALTER TABLE things ADD COLUMN name text;\n"),
		},
		"migrations/0003_pending.sql": {
			Data: []byte("CREATE TABLE should_not_run (id bigint);\n"),
		},
	}
	_, err := Migrate(ctx, pool, changed, "migrations")
	require.Error(t, err, "Migrate() with a changed migration")
	assert.ErrorContains(t, err, "changed")
	assertMigrationNotApplied(t, ctx, pool, "should_not_run", 2)
}

func TestMigrateRejectsBackfilledPendingMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	versionsOneAndThree := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0003_add_name.sql": {
			Data: []byte("ALTER TABLE things ADD COLUMN name text;\n"),
		},
	}
	if _, err := Migrate(ctx, pool, versionsOneAndThree, "migrations"); err != nil {
		require.NoError(t, err, "Migrate()")
	}

	backfilled := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0002_backfilled.sql": {
			Data: []byte("CREATE TABLE should_not_run (id bigint);\n"),
		},
		"migrations/0003_add_name.sql": {
			Data: []byte("ALTER TABLE things ADD COLUMN name text;\n"),
		},
	}
	_, err := Migrate(ctx, pool, backfilled, "migrations")
	require.Error(t, err, "Migrate() with a backfilled migration")
	assert.ErrorContains(t, err, "older than applied")
	assertMigrationNotApplied(t, ctx, pool, "should_not_run", 2)
}

func assertMigrationNotApplied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, wantRecords int) {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
		require.NoError(t, err, "check table %s", table)
	}
	assert.False(t, exists, "migration created %s even though preflight should have rejected the history", table)

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		require.NoError(t, err, "query schema_migrations")
	}
	assert.Equal(t, wantRecords, recorded, "schema_migrations count")
}

func TestMigrateRollsBackFailedMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	broken := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0002_broken.sql": {
			Data: []byte("CREATE TABLE should_rollback (id bigint);\nSELECT * FROM missing_table;\n"),
		},
	}
	applied, err := Migrate(ctx, pool, broken, "migrations")
	require.Error(t, err, "Migrate() with a broken migration")
	assert.Len(t, applied, 1, "migrations applied before failure")

	var rolledBack bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('should_rollback') IS NULL").Scan(&rolledBack); err != nil {
		require.NoError(t, err, "check rolled-back table")
	}
	assert.True(t, rolledBack, "failed migration left its table behind")

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		require.NoError(t, err, "query schema_migrations")
	}
	assert.Equal(t, 1, recorded, "schema_migrations count")

	fixed := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0002_broken.sql": {
			Data: []byte("CREATE TABLE should_rollback (id bigint);\n"),
		},
	}
	applied, err = Migrate(ctx, pool, fixed, "migrations")
	require.NoError(t, err, "Migrate() after fix error")
	assert.Len(t, applied, 1, "migrations applied after fix")
}

func TestMigrateRejectsMissingAppliedMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	if _, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations"); err != nil {
		require.NoError(t, err, "Migrate()")
	}

	missing := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
	}
	_, err := Migrate(ctx, pool, missing, "migrations")
	require.Error(t, err, "Migrate() with a missing applied migration")
	assert.ErrorContains(t, err, "missing")
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	return dsn
}

func newIntegrationSchemaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := integrationDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := Open(ctx, dsn)
	require.NoError(t, err, "open admin database pool")

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		admin.Close()
		require.NoError(t, err, "create test schema")
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		require.NoError(t, err, "parse test database URL")
	}
	poolConfig.MaxConns = 4
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		admin.Close()
		require.NoError(t, err, "create test schema pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		admin.Close()
		require.NoError(t, err, "ping test schema")
	}

	t.Cleanup(func() {
		pool.Close()

		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
		admin.Close()
	})

	return pool
}

func integrationMigrationFixture() fstest.MapFS {
	return fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0002_add_name.sql": {
			Data: []byte("ALTER TABLE things ADD COLUMN name text;\n"),
		},
	}
}
