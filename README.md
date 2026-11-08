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

Install the pinned toolchain and then run:

```sh
mise install
mise run deps
mise run format
mise run lint
mise run test
```

`check` runs the formatting check, linting, and unit tests together:

```sh
mise run check
```

Configuration is read from the environment. [.env.example](.env.example)
documents the available variables and local defaults. `mise` automatically
loads a local `.env` file for every task; do not commit it.

## Documentation

- [Implementation plan](docs/plans/20261108_201136_budget_tracker_mvp_plan.md)
- [Architecture decision records](docs/decisions/README.md)
- [Agent guidance](AGENTS.md)
