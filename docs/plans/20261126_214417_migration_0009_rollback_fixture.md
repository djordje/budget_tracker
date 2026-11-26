Status: Complete

# Migration 0009 rollback fixture

## Context and goal

Before this change, the rollback test in `backend/internal/store/schema_integration_test.go` started from a fully migrated schema, dropped the constraints added by `0009`, and deleted the migration records for `0009` and later to replay it. Without the fixture adjustment shipped alongside `0010`, migration preflight would reject pending `0009` while `0010` was recorded as applied. The deletions were confined to each subtest's private PostgreSQL schema, which `testdb.New` drops at cleanup.

This is a test-only follow-up to [Household currency choices](./20261124_223027_household_currency_choices.md). Production migration behavior and SQL are unchanged.

## Delivered fixture

1. `testdb.NewWithMigrations(t, migrations)` initializes a fresh schema from an explicitly supplied migration filesystem. `testdb.New(t)` delegates to it with the embedded migrations, so existing callers are unchanged.
2. `migrationsThrough(t, version)` in `backend/internal/store/schema_integration_test.go` builds a `testing/fstest.MapFS` containing the embedded migrations up to a version, each under its canonical filename with its embedded SQL unchanged, so the runner verifies the same checksums as production. The rollback test starts the schema with `0001`–`0008`, seeds the invalid legacy row, then runs `store.Migrate` with `0001`–`0009`. No later SQL runs, no constraints are dropped, and no migration records are deleted by hand. The previous `dropEntrySchemaConstraints` helper is gone.
3. The test keeps the existing assertions for both cases: the expected PostgreSQL error and constraint name, no surviving constraints from `0009`, the legacy row still present, and `0009` not recorded as applied.

## Validation

- `mise run lint` — passed (`go vet` and `golangci-lint` for the plain and `integration` build-tag modes, 0 issues).
- `mise run test` — passed.
- `mise run test:integration` — passed against local PostgreSQL; `TestMigration0009RollsBackInvalidLegacyRows` passed both the non-member income and over-long first name cases.
