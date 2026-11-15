package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbmigrations "budgettracker/db"
	"budgettracker/internal/config"
	"budgettracker/internal/store"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "status") {
		fmt.Fprintf(os.Stderr, "usage: %s <up|status>\n", os.Args[0])
		os.Exit(2)
	}

	if err := run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(command string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch command {
	case "up":
		return migrateUp(ctx, pool)
	case "status":
		return printStatus(ctx, pool)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func migrateUp(ctx context.Context, pool *pgxpool.Pool) error {
	applied, err := store.Migrate(ctx, pool, dbmigrations.Migrations, "migrations")
	if err != nil {
		for _, migration := range applied {
			fmt.Printf("applied %s\n", migration.Label())
		}
		return err
	}

	if len(applied) == 0 {
		fmt.Println("database is already up to date")
		return nil
	}

	fmt.Printf("applied %d migration(s):\n", len(applied))
	for _, migration := range applied {
		fmt.Printf("  %s\n", migration.Label())
	}
	fmt.Println("database is up to date")
	return nil
}

func printStatus(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := store.LoadMigrations(dbmigrations.Migrations, "migrations")
	if err != nil {
		return err
	}
	applied, err := store.AppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	return writeStatus(os.Stdout, migrations, applied)
}

// writeStatus renders the migration status table and reports the first write
// or flush error, so a truncated report never looks like a successful one.
func writeStatus(out io.Writer, migrations []store.Migration, applied []store.AppliedMigration) error {
	appliedByVersion := make(map[int64]store.AppliedMigration, len(applied))
	for _, migration := range applied {
		appliedByVersion[migration.Version] = migration
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "VERSION\tNAME\tSTATUS\tAPPLIED AT"); err != nil {
		return fmt.Errorf("write status header: %w", err)
	}

	known := make(map[int64]bool, len(migrations))
	for _, migration := range migrations {
		known[migration.Version] = true
		status := "pending"
		appliedAt := "-"
		if record, ok := appliedByVersion[migration.Version]; ok {
			status = "applied"
			appliedAt = record.AppliedAt.UTC().Format(time.RFC3339)
			if record.Checksum != migration.Checksum {
				status = "modified"
			}
		}
		if _, err := fmt.Fprintf(w, "%04d\t%s\t%s\t%s\n", migration.Version, migration.Name, status, appliedAt); err != nil {
			return fmt.Errorf("write status row: %w", err)
		}
	}

	for _, record := range applied {
		if !known[record.Version] {
			if _, err := fmt.Fprintf(w, "%04d\t%s\tmissing\t%s\n",
				record.Version, record.Name, record.AppliedAt.UTC().Format(time.RFC3339)); err != nil {
				return fmt.Errorf("write status row: %w", err)
			}
		}
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush status table: %w", err)
	}
	return nil
}
