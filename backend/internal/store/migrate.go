package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 0x627564676574 // "budget"

var migrationFilename = regexp.MustCompile(`^([0-9]{4,})_([a-z0-9][a-z0-9_-]*)\.sql$`)

const createSchemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version bigint PRIMARY KEY,
	name text NOT NULL,
	checksum text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migration is one numbered SQL migration.
type Migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

// Label returns the conventional versioned migration name.
func (m Migration) Label() string {
	return fmt.Sprintf("%04d_%s", m.Version, m.Name)
}

// AppliedMigration is a migration recorded in the database.
type AppliedMigration struct {
	Version   int64
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// LoadMigrations reads and validates numbered SQL migrations from dir.
func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory %q: %w", dir, err)
	}

	migrations := make([]Migration, 0, len(entries))
	seen := make(map[int64]string, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		matches := migrationFilename.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q: use NNNN_name.sql", entry.Name())
		}

		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", version, previous, entry.Name())
		}

		filePath := path.Join(dir, entry.Name())
		contents, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}

		sum := sha256.Sum256(contents)
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     matches[2],
			SQL:      string(contents),
			Checksum: hex.EncodeToString(sum[:]),
		})
		seen[version] = entry.Name()
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// Migrate applies pending migrations in order and returns the newly applied
// ones. Applied versions are recorded with a checksum so edited migrations are
// detected instead of executed out of order.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string) ([]Migration, error) {
	migrations, err := LoadMigrations(fsys, dir)
	if err != nil {
		return nil, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, createSchemaMigrationsTable); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedChecksums(ctx, conn)
	if err != nil {
		return nil, err
	}

	pending, err := preflight(migrations, applied)
	if err != nil {
		return nil, err
	}

	var newlyApplied []Migration
	for _, migration := range pending {
		if err := applyMigration(ctx, conn, migration); err != nil {
			return newlyApplied, fmt.Errorf("apply migration %s: %w", migration.Label(), err)
		}
		newlyApplied = append(newlyApplied, migration)
	}

	return newlyApplied, nil
}

// preflight validates the complete migration history before any pending
// migration is applied. It rejects missing applied files, changed applied
// checksums, and pending versions older than the greatest applied version.
func preflight(migrations []Migration, applied map[int64]string) ([]Migration, error) {
	loaded := make(map[int64]bool, len(migrations))
	for _, migration := range migrations {
		loaded[migration.Version] = true
	}

	var greatestApplied int64
	for version := range applied {
		if !loaded[version] {
			return nil, fmt.Errorf("database has applied migration %04d that is missing from the migration files", version)
		}
		if version > greatestApplied {
			greatestApplied = version
		}
	}

	pending := make([]Migration, 0, len(migrations))
	for _, migration := range migrations {
		if checksum, ok := applied[migration.Version]; ok {
			if checksum != migration.Checksum {
				return nil, fmt.Errorf(
					"migration %s was changed after it was applied; create a new migration instead",
					migration.Label(),
				)
			}
			continue
		}
		if migration.Version < greatestApplied {
			return nil, fmt.Errorf(
				"pending migration %s is older than applied migration %04d; migrations must be added in increasing order",
				migration.Label(), greatestApplied,
			)
		}
		pending = append(pending, migration)
	}

	return pending, nil
}

// AppliedMigrations returns recorded migrations in version order. A database
// without the tracking table has no applied migrations.
func AppliedMigrations(ctx context.Context, pool *pgxpool.Pool) ([]AppliedMigration, error) {
	var tableExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&tableExists); err != nil {
		return nil, fmt.Errorf("check schema_migrations: %w", err)
	}
	if !tableExists {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT version, name, checksum, applied_at
		FROM schema_migrations
		ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	var applied []AppliedMigration
	for rows.Next() {
		var migration AppliedMigration
		if err := rows.Scan(&migration.Version, &migration.Name, &migration.Checksum, &migration.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied = append(applied, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}

	return applied, nil
}

func appliedChecksums(ctx context.Context, conn *pgxpool.Conn) (map[int64]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}

	return applied, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, migration Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO schema_migrations (version, name, checksum)
		VALUES ($1, $2, $3)`,
		migration.Version, migration.Name, migration.Checksum,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
