package store

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMigrationsSortsByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_add_name.sql":      {Data: []byte("ALTER TABLE things ADD COLUMN name text;\n")},
		"migrations/0001_create_things.sql": {Data: []byte("CREATE TABLE things (id bigint PRIMARY KEY);\n")},
	}

	migrations, err := LoadMigrations(fsys, "migrations")
	require.NoError(t, err, "LoadMigrations()")
	require.Len(t, migrations, 2)

	assert.Equal(t, int64(1), migrations[0].Version, "first migration = %+v", migrations[0])
	assert.Equal(t, "create_things", migrations[0].Name, "first migration = %+v", migrations[0])
	assert.Equal(t, int64(2), migrations[1].Version, "second migration = %+v", migrations[1])
	assert.Equal(t, "add_name", migrations[1].Name, "second migration = %+v", migrations[1])

	assert.NotEmpty(t, migrations[0].Checksum, "checksums = %q, %q", migrations[0].Checksum, migrations[1].Checksum)
	assert.NotEqual(t, migrations[1].Checksum, migrations[0].Checksum, "checksums = %q, %q", migrations[0].Checksum, migrations[1].Checksum)

	assert.Equal(t, "0001_create_things", migrations[0].Label(), "Label()")
}

func TestLoadMigrationsRejectsInvalidFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/1_short_version.sql": {Data: []byte("SELECT 1;")},
	}

	_, err := LoadMigrations(fsys, "migrations")
	assert.Error(t, err, "LoadMigrations() with a short version prefix")
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_first.sql":  {Data: []byte("SELECT 1;")},
		"migrations/0001_second.sql": {Data: []byte("SELECT 2;")},
	}

	_, err := LoadMigrations(fsys, "migrations")
	require.Error(t, err, "LoadMigrations() with duplicate versions")
	assert.ErrorContains(t, err, "duplicate")
}

func TestLoadMigrationsIgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/.gitkeep": {Data: []byte{}},
	}

	migrations, err := LoadMigrations(fsys, "migrations")
	require.NoError(t, err, "LoadMigrations()")
	assert.Empty(t, migrations)
}
