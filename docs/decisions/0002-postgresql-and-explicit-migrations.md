# 0002: PostgreSQL with pgx and explicit SQL migrations

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Household membership, money, and exchange-rate rules need relational
constraints and transactional consistency. The project favors explicit,
inspectable behavior over an ORM, and future sessions must be able to reproduce
the schema from a fresh checkout.

## Decision

Use PostgreSQL, the `pgx` driver and connection pool, and numbered SQL migration
files applied by a small in-repository migration runner. Applied versions and
checksums are recorded in `schema_migrations`. Migrations are embedded into the
migration command so the same files run locally and in CI.

Generated query layers such as `sqlc` may be added later if they prove useful;
they are not a prerequisite.

## Consequences

- Schema changes are reviewed as SQL and applied in a deterministic order.
- The migration command is available through `mise run db:migrate`.
- Changing an already-applied migration is detected by its checksum and
  rejected; create a new migration instead.
- Integration tests use real PostgreSQL rather than mocks.
- Local rollback is handled by recreating the disposable Compose database until
  down migrations are needed.
