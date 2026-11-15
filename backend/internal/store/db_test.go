package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	ctx := context.Background()

	_, err := Open(ctx, "")
	assert.Error(t, err, `Open("")`)

	_, err = Open(ctx, "not a database URL")
	assert.Error(t, err, `Open("not a database URL")`)
}

func TestOpenHonorsCanceledContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping connection timeout in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Open(ctx, "postgres://budget@127.0.0.1:1/budget_tracker?sslmode=disable")
	assert.Error(t, err, "Open() with a canceled context")
}
