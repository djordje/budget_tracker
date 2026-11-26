// Package testdb creates isolated PostgreSQL schemas for integration tests.
package testdb

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbmigrations "budgettracker/db"
	"budgettracker/internal/store"
)

// New returns a pool whose search_path points at a freshly migrated schema
// built from the embedded migrations. It skips the test when
// TEST_DATABASE_URL is not set.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return NewWithMigrations(t, dbmigrations.Migrations)
}

// NewWithMigrations returns a pool whose search_path points at a fresh schema
// migrated from the supplied filesystem. It lets a test control which
// migrations the schema starts from. It skips the test when
// TEST_DATABASE_URL is not set.
func NewWithMigrations(t *testing.T, migrations fs.FS) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
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

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
		admin.Close()
	})

	if _, err := store.Migrate(ctx, pool, migrations, "migrations"); err != nil {
		t.Fatalf("apply test migrations: %v", err)
	}

	return pool
}
