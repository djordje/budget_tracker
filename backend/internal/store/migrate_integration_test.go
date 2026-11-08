//go:build integration

package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, integrationDatabaseURL(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	var value int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil {
		t.Fatalf("query database: %v", err)
	}
	if value != 1 {
		t.Errorf("SELECT 1 = %d, want 1", value)
	}
}

func TestMigrateAppliesAndTracksMigrations(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	applied, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations")
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %d migrations, want 2", len(applied))
	}

	if _, err := pool.Exec(ctx, "INSERT INTO things (id, name) VALUES (1, 'first')"); err != nil {
		t.Fatalf("insert migrated row: %v", err)
	}
	var name string
	if err := pool.QueryRow(ctx, "SELECT name FROM things WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if name != "first" {
		t.Errorf("name = %q, want first", name)
	}

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if recorded != 2 {
		t.Errorf("schema_migrations count = %d, want 2", recorded)
	}

	records, err := AppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("AppliedMigrations() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("AppliedMigrations() returned %d records, want 2", len(records))
	}
	if records[0].Version != 1 || records[1].Version != 2 {
		t.Errorf("AppliedMigrations() versions = %d, %d", records[0].Version, records[1].Version)
	}
	if records[0].AppliedAt.IsZero() {
		t.Error("AppliedMigrations() returned a zero applied time")
	}

	applied, err = Migrate(ctx, pool, integrationMigrationFixture(), "migrations")
	if err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("second Migrate() applied %d migrations, want 0", len(applied))
	}
}

func TestMigrateRejectsChangedMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	if _, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
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
	if err == nil {
		t.Fatal("Migrate() error = nil, want checksum error")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Errorf("error = %v, want changed migration error", err)
	}
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
		t.Fatalf("Migrate() error = %v", err)
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
	if err == nil {
		t.Fatal("Migrate() error = nil, want backfilled migration error")
	}
	if !strings.Contains(err.Error(), "older than applied") {
		t.Errorf("error = %v, want backfilled migration error", err)
	}
	assertMigrationNotApplied(t, ctx, pool, "should_not_run", 2)
}

func assertMigrationNotApplied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, wantRecords int) {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if exists {
		t.Errorf("migration created %s even though preflight should have rejected the history", table)
	}

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if recorded != wantRecords {
		t.Errorf("schema_migrations count = %d, want %d", recorded, wantRecords)
	}
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
	if err == nil {
		t.Fatal("Migrate() error = nil, want error")
	}
	if len(applied) != 1 {
		t.Errorf("applied %d migrations before failure, want 1", len(applied))
	}

	var rolledBack bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('should_rollback') IS NULL").Scan(&rolledBack); err != nil {
		t.Fatalf("check rolled-back table: %v", err)
	}
	if !rolledBack {
		t.Error("failed migration left its table behind")
	}

	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if recorded != 1 {
		t.Errorf("schema_migrations count = %d, want 1", recorded)
	}

	fixed := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
		"migrations/0002_broken.sql": {
			Data: []byte("CREATE TABLE should_rollback (id bigint);\n"),
		},
	}
	applied, err = Migrate(ctx, pool, fixed, "migrations")
	if err != nil {
		t.Fatalf("Migrate() after fix error = %v", err)
	}
	if len(applied) != 1 {
		t.Errorf("applied %d migrations after fix, want 1", len(applied))
	}
}

func TestMigrateRejectsMissingAppliedMigration(t *testing.T) {
	pool := newIntegrationSchemaPool(t)
	ctx := context.Background()

	if _, err := Migrate(ctx, pool, integrationMigrationFixture(), "migrations"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	missing := fstest.MapFS{
		"migrations/0001_create_things.sql": {
			Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n"),
		},
	}
	_, err := Migrate(ctx, pool, missing, "migrations")
	if err == nil {
		t.Fatal("Migrate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want missing migration error", err)
	}
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
	if err != nil {
		t.Fatalf("open admin database pool: %v", err)
	}

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("parse test database URL: %v", err)
	}
	poolConfig.MaxConns = 4
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		admin.Close()
		t.Fatalf("create test schema pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		admin.Close()
		t.Fatalf("ping test schema: %v", err)
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
