# Budget Tracker

A small, multi-user household finance application for recording income and
spending, converting totals into a preferred currency, and viewing personal or
household-level summaries.

The project is a Go application with a server-rendered HTML interface, a
versioned JSON API, and PostgreSQL persistence. The implementation plan lives
in [docs/plans/20261108_201136_budget_tracker_mvp_plan.md](docs/plans/20261108_201136_budget_tracker_mvp_plan.md).

**Status:** Milestone 4 (invitations, preferences, and UX polish) is complete.
The final milestone covers hardening, smoke coverage, and documentation.

## Requirements

- [mise](https://mise.jdx.dev/) for Go, Node.js, and development tasks
- Docker with Compose for local PostgreSQL

## Current commands

Install the pinned toolchain and prepare the environment:

```sh
mise install
mise run setup
mise run run
```

`setup` copies `.env.example` to `.env` when needed, installs dependencies,
compiles the browser assets, starts PostgreSQL, and applies migrations. The
application is available at <http://localhost:8080/app>. Health checks are
served at `/healthz` (liveness), `/readyz` (database readiness), and
`/api/v1/health` (versioned liveness alias).

Other development tasks:

```sh
mise run format       # format Go source
mise run lint         # run go vet
mise run test         # run unit tests
mise run build        # build bin/budget-tracker
mise run css:build    # compile Tailwind CSS
mise run assets:build # compile CSS and copy HTMX
mise run css:watch    # rebuild CSS while editing templates
mise run db:status    # show applied and pending migrations
mise run db:down      # stop local PostgreSQL
```

`check` runs the formatting check, linting, unit tests, and a build together:

```sh
mise run check
```

With PostgreSQL running, `check:all` also runs the database integration tests:

```sh
mise run test:integration
mise run check:all
```

Configuration is read from the environment. [.env.example](.env.example)
documents the available variables; `DATABASE_URL` is required in every
environment. `mise` automatically loads a local `.env` file for every task, and
`mise run setup` copies `.env.example` when needed. Do not commit `.env`.
Running the server outside `mise` without `DATABASE_URL` fails clearly at
startup. `APP_ENV` accepts only `development` or `production`; `LOG_LEVEL`
accepts `debug`, `info`, `warn`, or `error`. `WEB_STATIC_DIR` is optional and
empty by default.

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | `development` or `production`; `production` uses JSON logs. |
| `HTTP_ADDR` | `:8080` | HTTP listen address. |
| `DATABASE_URL` | required | Required in every environment; never commit a real URL. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `WEB_STATIC_DIR` | empty | Optional absolute path for disk assets; empty uses the embedded copies. |
| `TEST_DATABASE_URL` | local Compose URL | Used by PostgreSQL integration tests. |

The Compose PostgreSQL instance binds to `127.0.0.1:5433` and uses trust
authentication for local development only. It is not intended for production.

## Testing

Unit tests do not require PostgreSQL:

```sh
mise run test
```

Integration tests run against a real database and create isolated schemas, so
they do not disturb development data:

```sh
mise run db:up
mise run test:integration
```

`mise run check` covers formatting, linting, unit tests, and a build.
`mise run check:all` adds the PostgreSQL integration tests. CI runs the same
migration and check tasks on every push and pull request.

## Accounts and households

Registration, sign-in, sign-out, household creation, household switching, and
admin-only household settings are implemented. A user can belong to multiple
households; the selected household is stored on the session by token hash.
Creating a household inserts the household, its admin membership, default
options, and the active-household preference in one transaction, so an
activation failure leaves no household behind. Household options default to
RSD; editable currency preferences arrive with the preferences milestone.
Expired sessions are removed opportunistically after successful registration
or login, at most once per hour per process.

Current HTML routes:

```text
/                              landing page
/register, /login              account flows
/app                           dashboard
/app/households/new            create a household
/app/households/{id}/settings  admin settings and members
/app/entries                   filtered entry list
/app/incomes/new               add income
/app/spendings/new             add spending
/app/incomes/{id}/edit         edit income
/app/spendings/{id}/edit       edit spending
/app/exchange-rates            manage exact and monthly rates
/app/reports                   converted household summary
/app/preferences               personal currency overrides
/invitations/{token}           public invitation acceptance
```

Current JSON endpoints (cookie authentication, CSRF header on unsafe methods):

```text
GET  /api/v1/households
POST /api/v1/households
GET  /api/v1/households/{id}
GET  /api/v1/households/{id}/members   admin only
GET  /api/v1/households/{id}/options   members
PUT  /api/v1/households/{id}/options   admin only
GET  /api/v1/households/{id}/my-options
PUT  /api/v1/households/{id}/my-options
GET  /api/v1/households/{id}/invitations   admin only
POST /api/v1/households/{id}/invitations   admin only
DELETE /api/v1/households/{id}/invitations/{invitation_id}
GET  /api/v1/currencies
GET  /api/v1/incomes
POST /api/v1/incomes
GET  /api/v1/incomes/{id}
PATCH /api/v1/incomes/{id}
DELETE /api/v1/incomes/{id}
GET  /api/v1/spendings
POST /api/v1/spendings
GET  /api/v1/spendings/{id}
PATCH /api/v1/spendings/{id}
DELETE /api/v1/spendings/{id}
GET  /api/v1/exchange-rates
POST /api/v1/exchange-rates
PATCH /api/v1/exchange-rates/{id}
DELETE /api/v1/exchange-rates/{id}
GET  /api/v1/reports/summary?from=&to=&currency=&scope=&household_id=
```

Entry lists accept `scope=mine|all`, `from`, `to`, `page`, and `page_size`.
Members can only use `mine`; admins can view the whole household with `all`.
Household-wide entries are admin-only to create, view, edit, and delete.

## Invitations and preferences

Admins invite members by email and role and receive a one-time invitation link
to share manually; email delivery is not part of the MVP. Invitation tokens
are hashed at rest, expire after seven days, are single-use, and require the
accepting account's normalized email to match. Acceptance creates the
membership and makes the household active.

Household currency defaults are admin-managed. Each member can override
income, spending, and display currencies for a household or inherit the
defaults. Entry forms and reports use the resolved effective values.

## Income and spending

Amounts are parsed from decimal strings into integer minor units using the
currency's exponent. The original amount and currency are preserved. Household
defaults supply the preselected currency and date, and the current user is the
preselected attribution. The entry list supports type tabs, date filters,
personal/all scopes for admins, pagination, empty states, and progressive HTMX
updates with non-JavaScript form fallbacks.

Entry IDs are positive integers and JSON represents them as decimal strings.
Invalid, zero, negative, or overflowing route IDs are rejected before any
service call. Unrepresentable pagination offsets and invalid UTF-8 or NUL in
descriptions return validation feedback instead of database errors. Failed
HTMX deletes return an error fragment with the original status rather than a
success notice.

## Exchange rates and reports

Households maintain exact-date and monthly exchange rates. Exact-date rates
apply only on their date; monthly rates apply across their calendar month.
Direct rates take precedence over inferred inverses, and conversion never
chains currencies. All members can manage rates. Exchange-rate IDs use the
same positive integer route and decimal-string JSON representation as entries.

The report page and `GET /api/v1/reports/summary` convert entries into a chosen
display currency. Each summary is one database snapshot: display options,
currencies, entries, and rates are read in a read-only `REPEATABLE READ`
transaction. Totals are incomplete and list the missing currency pairs and
entry dates when a required rate is unavailable; affected entries are never
treated as zero. Admins can switch between their data and the whole household;
members see only their own data.

## Frontend assets

Tailwind CSS is compiled at build time from
`backend/web/assets/input.css` into `backend/web/static/css/app.css`, and
HTMX is copied from `node_modules` into `backend/web/static/js/htmx.min.js`.
Generated assets are ignored by git and embedded in the Go executable.
`mise run run` and `mise run build` rebuild them automatically. In development,
`mise run run` serves assets from disk, so `mise run css:watch` updates styling
without restarting the server. Production builds use the embedded copies.

## Database migrations

Migrations are numbered SQL files in `backend/db/migrations` named
`NNNN_description.sql`, applied in version order. Applied versions and their
checksums are tracked in `schema_migrations`; never edit a migration that has
already been applied. Add a new migration instead.

```sh
mise run db:migrate  # apply pending migrations
mise run db:status   # list applied and pending migrations
mise run db:reset    # destroy and recreate the local database
```

## Project structure

```text
backend/
  cmd/server/          HTTP server entrypoint
  cmd/migrate/         migration command
  db/migrations/       numbered SQL migrations
  internal/auth/       credentials, sessions, cookies, and middleware
  internal/config/     environment configuration
  internal/finance/    currencies, money, entries, and exchange rates
  internal/households/ household services and authorization
  internal/httpapi/    JSON handlers, health, CSRF, and middleware
  internal/logging/    structured logging setup
  internal/reporting/  exact conversion and summary reports
  internal/server/     HTTP composition and route registration
  internal/store/      PostgreSQL pool, transactions, and migration runner
  internal/testdb/     isolated PostgreSQL schemas for integration tests
  internal/web/        HTML handlers and template rendering
  web/                 templates, Tailwind source, and static assets
docs/
  decisions/           architecture decision records
  plans/               implementation plan and planning conventions
```

## Documentation

- [Implementation plan](docs/plans/20261108_201136_budget_tracker_mvp_plan.md)
- [Architecture decision records](docs/decisions/README.md)
- [Agent guidance](AGENTS.md)
