# AGENTS.md

## Project context

Budget Tracker is multi-user household finance application written in Go. Prefer small, explicit, maintainable solutions over frameworks or abstractions that hide application behavior.

Before changing the product or architecture, read:

- `docs/plans/README.md` for planning conventions and the plan index.

Keep the active plan current when decisions change. Follow its timestamped naming convention for new plans; do not create a new file for ordinary revisions.

## Technology and local environment

- Use Go for the backend and PostgreSQL for persistence.
- Start with standard-library `net/http`, `html/template`, and `slog`; use `pgx` and explicit SQL migrations. Add libraries only when they solve a concrete problem.
- Build responsive server-rendered HTML with Tailwind CSS and progressive HTMX enhancements. Do not introduce a SPA or a separate frontend project.
- Serve HTML under `/app`, versioned JSON under `/api/v1`, and static assets from one Go executable.
- Use `mise` for Go, tool versions, and development tasks. Run local PostgreSQL in Docker through Compose.
- Never commit credentials, session tokens, invitation tokens, `.env` files containing secrets, or other sensitive data.

## Architecture and implementation rules

- Keep HTTP handlers thin: parse and validate input, call application services, and render the response.
- Put business rules, authorization, and database transactions in application services shared by HTML handlers and JSON handlers. Do not implement the same rules independently in each adapter.
- Organize packages around behavior such as `auth`, `households`, `finance`, and `reporting`; avoid large generic `models` or `utils` packages.
- Scope every household-owned query by `household_id` and enforce authorization in the service layer. Never trust a client-supplied household or resource ID without an access check.
- Keep credentials and sessions separate from the public `users` table. Use normalized, unique email addresses for login.
- Preserve the confirmed role rules: members manage their own entries; only admins can view all entries, manage household settings/invitations, and modify household-wide entries.

## Money, exchange rates, and UI invariants

- Parse money from decimal strings, never floating point. Store original amounts as positive integer minor units together with their currency.
- Respect each currency’s minor-unit exponent; do not assume two decimal places.
- Store exchange rates as exact decimals. Use exact-date rates only on that date and monthly rates only within their calendar month.
- Prefer exact-date over monthly rates and direct over inferred-inverse rates at the same scope/date. Infer inverse rates as `1 / rate`; do not use chained conversion.
- Keep intermediate conversion arithmetic exact. Use half-even rounding only when producing the display currency’s minor-unit precision.
- Never silently omit entries with missing exchange rates. Mark affected reports incomplete and identify what is missing.
- Treat household-wide entries as separate from “my data.”
- Distinguish optional fields from required fields with defaults. Show sensible defaults visibly preselected and editable. An entry should require only amount input when date, currency, and attribution defaults are available; description remains optional.

## Commit and delivery discipline

Every commit must leave the application in a working, buildable, and testable state. Keep commits small and coherent so history is easy to review; do not build an entire feature or milestone in one commit.

Order commits so each change depends only on behavior already present in its parent:

1. Add a compatible database migration before models, generated queries, or services that require the new schema.
2. Add or update the model/query layer before services that require it.
3. Add application services before API handlers that require them.
4. Add or update API handlers before UI code that calls those endpoints.
5. Add UI components only after their required service/API behavior exists.

For example, migration → model/query → service → API → UI is a valid sequence. UI that calls a nonexistent API, or code that references a schema introduced by a later commit, is not. Adding an API before its UI is valid even if the UI arrives in a later commit.

Additional requirements:

- Include relevant tests and validation updates in the same commit as the behavior they cover rather than postponing all tests to a final commit.
- Ensure migrations run successfully and do not break the application at the commit where they are introduced.
- Prefer backward-compatible intermediate schema changes when existing code still depends on the previous schema.
- Do not commit generated artifacts unless the repository deliberately tracks them.
- Run the available formatting, linting, unit, integration, and smoke checks relevant to the change. Never claim checks passed if they were not run.
- For documentation-only commits, review the document and links; application tests are not required.

When asked to prepare or create a commit, follow `.agents/skills/commit-message/SKILL.md`.

## Documentation and testing

- Use `github.com/stretchr/testify` (`assert`, `require`) for assertions in `backend` tests instead of hand-written `if got != want` checks. Pass the expected value first, then the actual value: `require.Equal(t, want, got)`.
- Use `require` when a failure would make the rest of the test meaningless — setup, JSON decode, a gating status code, or any value that is indexed, dereferenced, or type-asserted afterwards. Use `assert` for independent checks, including expected errors that no later check depends on. Never call `require`, `t.Fatal`, or `t.FailNow` from a worker goroutine: report nonfatally and return.
- Keep hand-written helpers that encode a protocol, such as request builders and fixtures. Do not add helpers that wrap a single comparison; Testify already provides them.
- `mise run lint` runs `go vet` and `golangci-lint` for the plain and `integration` build-tag modes; integration-tagged test files are only checked by the second mode, so both runs are required. `backend/.golangci.yml` enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, `testifylint`, and `unused` explicitly over `default: none`. Changing that configuration is a plan-level decision, not a local one: loosening it hides regressions, and widening it surfaces findings that belong to the plan that fixes them.
- Update the active plan when confirmed product decisions or milestones change. Completed plans in `docs/plans` are read-only historical records: never rewrite their delivered milestones, acceptance criteria, or validation status. Record new findings in the active plan or a new plan that links back. Follow `docs/plans/README.md` for status and lifecycle rules.
- Document local setup and development tasks as they are introduced; keep commands in documentation synchronized with `mise.toml`.
- Test money arithmetic, rate precedence, inverse conversion, rounding, date boundaries, and missing-rate handling with table-driven tests.
- Use real PostgreSQL for persistence and transaction integration tests where practical.
- Include authorization tests for both ownership rules and cross-household access attempts.
- Exercise HTML and JSON adapters against the same services, including HTMX fragment responses and validation errors.

