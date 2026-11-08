package store

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsSortsByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_add_name.sql":      {Data: []byte("ALTER TABLE things ADD COLUMN name text;\n")},
		"migrations/0001_create_things.sql": {Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n")},
	}

	migrations, err := LoadMigrations(fsys, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("got %d migrations, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[0].Name != "create_things" {
		t.Errorf("first migration = %+v", migrations[0])
	}
	if migrations[1].Version != 2 || migrations[1].Name != "add_name" {
		t.Errorf("second migration = %+v", migrations[1])
	}
	if migrations[0].Checksum == "" || migrations[0].Checksum == migrations[1].Checksum {
		t.Errorf("checksums = %q, %q", migrations[0].Checksum, migrations[1].Checksum)
	}
	if got := migrations[0].Label(); got != "0001_create_things" {
		t.Errorf("Label() = %q", got)
	}
}

func TestLoadMigrationsRejectsInvalidFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/1_short_version.sql": {Data: []byte("SELECT 1;")},
	}

	if _, err := LoadMigrations(fsys, "migrations"); err == nil {
		t.Fatal("LoadMigrations() error = nil, want error")
	}
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_first.sql":  {Data: []byte("SELECT 1;")},
		"migrations/0001_second.sql": {Data: []byte("SELECT 2;")},
	}

	_, err := LoadMigrations(fsys, "migrations")
	if err == nil {
		t.Fatal("LoadMigrations() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error = %v, want duplicate version error", err)
	}
}

func TestLoadMigrationsIgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/.gitkeep": {Data: []byte{}},
	}

	migrations, err := LoadMigrations(fsys, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(migrations) != 0 {
		t.Errorf("got %d migrations, want 0", len(migrations))
	}
}
