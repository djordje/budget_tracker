package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/store"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func statusFixture() ([]store.Migration, []store.AppliedMigration) {
	migrations := []store.Migration{
		{Version: 1, Name: "create_things", Checksum: "aaa"},
		{Version: 2, Name: "add_more", Checksum: "bbb"},
	}
	applied := []store.AppliedMigration{
		{
			Version:   2,
			Name:      "add_more",
			Checksum:  "bbb",
			AppliedAt: time.Date(2026, 11, 14, 19, 28, 41, 0, time.UTC),
		},
	}
	return migrations, applied
}

func TestWriteStatusReportsOutputErrors(t *testing.T) {
	migrations, applied := statusFixture()
	writeErr := errors.New("no space left on device")

	err := writeStatus(failingWriter{err: writeErr}, migrations, applied)

	require.ErrorIs(t, err, writeErr, "status output error")
}

func TestWriteStatusRendersTable(t *testing.T) {
	migrations, applied := statusFixture()
	var out bytes.Buffer

	require.NoError(t, writeStatus(&out, migrations, applied), "write status")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3, "status table lines")
	assert.Equal(t, []string{"0001", "create_things", "pending", "-"}, strings.Fields(lines[1]), "pending row")
	assert.Equal(t, []string{"0002", "add_more", "applied", "2026-11-14T19:28:41Z"}, strings.Fields(lines[2]), "applied row")
}
