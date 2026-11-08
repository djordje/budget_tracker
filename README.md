# Budget Tracker

A small, multi-user household finance application for recording income and
spending, converting totals into a preferred currency, and viewing personal or
household-level summaries.

The project is a Go application with a server-rendered HTML interface, a
versioned JSON API, and PostgreSQL persistence. The implementation plan lives
in [docs/plans/20261108_201136_budget_tracker_mvp_plan.md](docs/plans/20261108_201136_budget_tracker_mvp_plan.md).

**Status:** Milestone 0 (project bootstrap) is in progress.

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
compiles Tailwind CSS, starts PostgreSQL, and applies migrations. The
application shell is available at <http://localhost:8080/app>. Health checks are
served at `/healthz`, `/readyz`, and `/api/v1/health`.

Other development tasks:

```sh
mise run format       # format Go source
mise run lint         # run go vet
mise run test         # run unit tests
mise run build        # build bin/budget-tracker
mise run css:build    # compile Tailwind CSS
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

The Compose PostgreSQL instance binds to `127.0.0.1:5433` and uses trust
authentication for local development only. It is not intended for production.

## Frontend assets

Tailwind CSS is compiled at build time from
`backend/web/assets/input.css` into
`backend/web/static/css/app.css`. The generated CSS is ignored by git and is
embedded in the Go executable. `mise run run` and `mise run build` rebuild it
automatically; use `mise run css:watch` in a second terminal while iterating on
templates.

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

## Documentation

- [Implementation plan](docs/plans/20261108_201136_budget_tracker_mvp_plan.md)
- [Architecture decision records](docs/decisions/README.md)
- [Agent guidance](AGENTS.md)
