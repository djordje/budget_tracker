package store

import (
	"context"
	"testing"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	ctx := context.Background()

	if _, err := Open(ctx, ""); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if _, err := Open(ctx, "not a database URL"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
}

func TestOpenHonorsCanceledContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping connection timeout in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Open(ctx, "postgres://budget@127.0.0.1:1/budget_tracker?sslmode=disable"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
}
