package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation. When constraint is non-empty, the violated constraint must match.
func IsUniqueViolation(err error, constraint string) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23505" {
		return false
	}
	return constraint == "" || pgError.ConstraintName == constraint
}
