Status: Proposed

# Migration 0009 rollback fixture

## Context and goal

The rollback test in `backend/internal/store/schema_integration_test.go` starts from a fully migrated schema and removes constraints and migration records to replay `0009`. Without the fixture adjustment shipped alongside `0010`, migration preflight would reject pending `0009` while `0010` was recorded as applied. The adjustment clears the records for `0009` and later, but leaves later schema changes in place. The deletions are confined to each subtest's private PostgreSQL schema, which `testdb.New` drops at cleanup.

This is a test-only follow-up to [Household currency choices](./20261124_223027_household_currency_choices.md). Keep production migration behavior and SQL unchanged.

## Proposed fixture

1. Let the test database helper initialize a fresh schema from an explicitly supplied migration filesystem. Keep `testdb.New(t)` applying all embedded migrations for existing callers.
2. Use bounded views of the real embedded SQL: apply only `0001`–`0008` during setup, seed the invalid legacy row, then run `store.Migrate` with `0001`–`0009`. Use the original filenames and bytes so the runner verifies the same checksums. Do not apply later SQL, remove constraints, or delete migration records by hand.
3. Retain the existing assertions for both cases: the expected PostgreSQL error and constraint name, no surviving constraints from `0009`, the legacy row still present, and `0009` not recorded as applied.

## Delivery and validation

- If approved, implement the bounded fixture as a focused follow-up to the currency changes. Run `mise run lint` and `mise run test:integration` against PostgreSQL with the test database configured.
