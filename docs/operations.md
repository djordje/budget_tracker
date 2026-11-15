# Local operations

This project is intentionally local-first. Deployment, hosting, and production
operations are outside the MVP.

## Setup and run

Requirements: `mise` and Docker with Compose.

```sh
mise install
mise run setup
mise run dev
```

`setup` installs Go and frontend dependencies, compiles Tailwind and copies
HTMX, starts PostgreSQL on `127.0.0.1:5433`, and applies migrations.

Open <http://localhost:8080/app>. Operational endpoints:

- `GET /healthz` — process liveness.
- `GET /readyz` — PostgreSQL availability.
- `GET /static/*` — embedded/browser assets.

## Common commands

```sh
mise run check:all      # format, vet, unit tests, PostgreSQL tests, build
mise run test           # unit tests only
mise run test:integration
mise run db:migrate
mise run db:status
mise run db:reset       # destroys local data
mise run assets:build
mise run css:watch      # serve CSS updates from disk during development
```

Migrations are numbered SQL files in `backend/db/migrations`. Applied versions
and checksums are tracked in `schema_migrations`; never edit an applied
migration.

## Test conventions

The rules live in `AGENTS.md` ("Documentation and testing"): Testify
`assert`/`require`, expected value before actual value, `require` for
prerequisites and `assert` for independent checks, and no assertion helpers that
wrap a single comparison.

`mise run lint` runs `go vet` and `golangci-lint` for both build-tag modes. The
configuration enables `errcheck`, `govet`, `ineffassign`, `staticcheck`,
`testifylint`, and `unused` explicitly over `default: none`, with no exclusions
or `nolint` directives.

```sh
mise run test                # unit tests, no database
mise run test:integration    # requires Compose PostgreSQL (mise run db:up)
cd backend && golangci-lint run ./...
cd backend && golangci-lint run --build-tags=integration ./...
```

Integration-tagged files are linted only by the second invocation, so run both.
The one rule `testifylint` cannot check is the ban on `require` inside worker
goroutines; report those nonfatally and return instead.

## Security controls

- Argon2id password hashes; normalized unique emails; generic login failures.
- Opaque session tokens stored only as SHA-256 hashes. Cookies are HTTP-only,
  SameSite=Lax, and Secure in production. Sessions expire after 30 days.
- Double-submit CSRF tokens for form and `X-CSRF-Token` submissions.
- Strict same-origin CSP, frame denial, no-referrer, permissions policy, and
  `no-store` caching for application responses. HSTS is emitted when the server
  runs with production cookie settings.
- Invitation tokens are hashed at rest, expire after seven days, are single
  use, require an email match, and are redacted from request logs.
- Failed logins and registration attempts are throttled in process at five
  attempts per IP/email pair per fifteen minutes. Admission atomically reserves
  capacity, keys are fixed-size hashes, and idle state is pruned. There is no
  oldest-key eviction: at the 10,000-key hard cap a new key is refused, and one
  client IP may create at most 50 new buckets per window, so junk keys cannot
  exhaust the shared cap or reset another client's counter. Registration spends
  that budget only after the password confirmation check passes and before any
  password hashing.
- Sessions use the token hash as the primary key. Expired sessions are removed
  opportunistically after successful registration or login, at most once per
  hour per process, and cleanup failures do not invalidate the new session.
- Request IDs, structured logs, panic recovery, timeouts, and graceful shutdown
  are enabled in the server composition root.

## Configuration and data invariants

- `DATABASE_URL` is required in every environment; `.env.example` documents the
  local Compose URL and `mise` loads it. `APP_ENV` accepts only `development` or
  `production`, and invalid values fail at startup.
- `DATABASE_URL` is never embedded as a fallback. Bare `go run` without
  configuration fails clearly; use the documented `mise` tasks.
- Migrations 0002–0008 were rewritten during remediation. Recreate disposable
  local and test databases rather than applying the edited files over an
  old-migrated database. CI and integration tests always start from a fresh
  isolated schema.
- Entry and exchange-rate IDs are positive integers. JSON exposes them as
  decimal strings; route parsing rejects malformed, zero, negative, and
  overflowing values before service calls.
- JSON request bodies use `application/json` (or a `+json` structured suffix),
  exactly one top-level object, and a 1 MiB limit. Malformed input is 400,
  oversized input is 413, and unsupported media types are 415 with safe
  envelopes. HTML form bodies share that 1 MiB bound; an oversized form is 413
  whether its length is declared or streamed as chunked.
- Household creation and activation commit in one transaction. Personal option
  updates return the committed personal and effective view from the same
  transaction. Report summaries read options, currencies, entries, and rates in
  one read-only `REPEATABLE READ` snapshot.
- Template changes require `mise run assets:build` and the rendering/handler
  tests. Semantic component classes and shared partials preserve appearance;
  they are not a component framework.

## Known limitations

- The login throttle is in-memory, bounded, and per process. Multi-instance
  deployment still needs a shared limiter.
- Email delivery is not configured. Admins receive an invitation link to copy
  and share manually.
- Exchange rates are entered manually; automatic providers, global/shared
  rates, rolling-window rates, and chained conversion are deferred.
- The browser smoke coverage is represented by HTTP integration tests. A real
  browser automation suite is not part of this local MVP.
- Hard deletion is used; audit history and soft deletion are deferred.
- MCP write access and native clients are not implemented.
- Deployment, TLS termination, backups, and production monitoring are outside
  this phase.

## MVP acceptance checks

The checklist in the active plan maps to automated coverage:

- Registration, login/logout, CSRF, household creation, and role boundaries:
  `internal/server` integration tests.
- Registration and session behavior: `internal/auth` tests.
- Household, invitation, and currency preference authorization:
  `internal/households` tests.
- Entry ownership, household-wide visibility, and cross-household denial:
  `internal/finance` and `internal/server` tests.
- Rate precedence, exact conversion, rounding, date boundaries, and missing
  rates: `internal/reporting` table-driven and PostgreSQL tests.
- End-to-end onboarding-to-report path: `TestMVPWorkflowSmoke`.

## Troubleshooting

- If PostgreSQL fails to start, check that port `5433` is free and run
  `docker compose logs postgres`.
- If CSS or HTMX is stale, run `mise run assets:build`; generated assets are
  intentionally not committed.
- `mise run dev` serves assets from disk using an absolute `WEB_STATIC_DIR`.
  The built `./bin/budget-tracker` uses embedded assets; do not set a relative
  `WEB_STATIC_DIR` unless running from the directory it is relative to. If a
  configured development directory is missing, the server logs a warning and
  falls back to the embedded assets.
- If schema state is confusing locally after migration edits,
  `mise run db:reset` recreates the disposable database and applies all
  migrations.
