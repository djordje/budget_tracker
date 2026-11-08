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

Install the pinned toolchain, start the local database, and run the server:

```sh
mise install
mise run deps
mise run db:up
mise run run
```

The application is available at <http://localhost:8080>. Health checks are
served at `/healthz`, `/readyz`, and `/api/v1/health`.

Other development tasks:

```sh
mise run format      # format Go source
mise run lint        # run go vet
mise run test        # run unit tests
mise run build       # build bin/budget-tracker
mise run db:down     # stop local PostgreSQL
```

`check` runs the formatting check, linting, unit tests, and a build together:

```sh
mise run check
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

## Documentation

- [Implementation plan](docs/plans/20261108_201136_budget_tracker_mvp_plan.md)
- [Architecture decision records](docs/decisions/README.md)
- [Agent guidance](AGENTS.md)
