# Budget Tracker

## 1. Product vision

A small, multi-user budget tracker for recording household income and spending, converting totals into a preferred currency, and viewing either personal or household-level summaries.

This is a greenfield project and a Go learning project. Prefer simple, explicit solutions over frameworks and abstractions that hide how the application works.

### Why an API and a server-rendered web app?

- A responsive web app provides a good starting experience on mobile and desktop without maintaining separate clients.
- A JSON API allows future native clients, integrations, and automation without rebuilding the domain layer.
- Server-rendered HTML, progressively enhanced with HTMX, keeps the first version small while supporting partial page updates.
- Domain operations should be reusable by HTML handlers, API handlers, and a future MCP server. MCP tools should call application services, not query the database directly.

Example future MCP interaction: “I spent 1500 RSD on groceries yesterday.” The agent should turn that into a structured `add_spending` tool call with amount, currency, date, and description. Ambiguous input should result in a clarification or confirmation, not a guessed transaction.

## 2. MVP scope

### Included

- Register, log in, and log out using email and password.
- Create a household and become its first admin.
- Invite a user to a household and accept an invitation.
- Record, edit, list, and delete incomes and spendings.
- Attribute an entry to a user or leave it household-wide.
- Configure household defaults and per-user currency overrides.
- Maintain dated and monthly household exchange rates.
- View income, spending, and net totals for a date range in a display currency.
- Switch between “my data” and permitted household-wide data.
- Responsive, accessible server-rendered UI with targeted HTMX updates.
- JSON endpoints for the core finance operations and summary report.

### Deliberately deferred

- Bank imports, receipt scanning, recurring entries, transfers, budgets/goals, categories and tags.
- Third-party identity providers and native applications.
- Advanced permissions, audit history, and soft deletion.
- Automatic exchange-rate providers, global/shared rates, rolling-window rates, and chained/cross-currency conversion.
- MCP write access. Design for it now; implement it after the core workflows are stable.

These are candidates for later milestones, not reasons to complicate the initial schema now. A nullable description on incomes and spendings is included in the MVP because it makes entries understandable and supports future natural-language input.

## 3. Recommended starting decisions

These are working defaults to validate before implementation, not requirements to preserve if a simpler alternative emerges.

| Topic | Recommendation |
| --- | --- |
| Application shape | One Go executable serving HTML under `/app`, JSON under `/api/v1`, and static assets. |
| Domain reuse | HTTP handlers parse/validate requests and call application services; services own transactions and authorization rules. |
| Router | Start with Go’s `net/http` ServeMux; add a small router only if it becomes limiting. |
| Templates | Standard `html/template` with layouts, pages, and reusable HTMX fragments. |
| Browser interactivity | HTMX first; add a tiny amount of vanilla JS only where HTMX is insufficient. Avoid introducing a SPA. |
| Styling | Tailwind CSS compiled at build time; no runtime CDN dependency in production. |
| Database | PostgreSQL, `pgx`, SQL migrations, and type-safe query generation with `sqlc` if it proves useful. |
| IDs | UUID primary keys; `timestamptz` for audit timestamps and `date` for financial dates. |
| Money | Signed `bigint` minor units in Go/PostgreSQL, with positive amounts required on both income and spending rows. |
| Exchange rates | Exact decimal values, never floating point. Use PostgreSQL `numeric` and a proven decimal type in Go. |
| Authentication | Local password credentials and database-backed sessions, isolated from the public `users` domain model. |
| Authorization | Household membership plus role checks in services and household-scoped database queries. |
| API contract | Versioned JSON endpoints with consistent validation/error responses; document stable endpoints with OpenAPI. |
| Configuration | Environment variables with local examples; no secrets committed to the repository. |
| Observability | Structured `slog` logging, request IDs, health/readiness endpoints, and centralized error handling. |

Go comparisons for orientation: application services resemble Rails service objects or NestJS providers, migrations remain explicit like Rails migrations, `pgx` is lower-level than ActiveRecord, and `html/template` plus HTMX is closer to classic server-rendered Rails views than to React.

## 4. Domain and data model

### General conventions

- Every household-owned row includes `household_id`; authorization must never rely on an ID supplied by the client alone.
- All tables have `created_at` and `updated_at` unless their purpose makes them unnecessary (for example, a join table).
- Currency references use a seeded `currencies` table with an ISO 4217 alphabetic code such as `RSD`, `EUR`, or `USD`.
- Validate foreign keys, enum-like values, uniqueness, and positive amounts in both the database and application where practical.

### Core tables

#### `users`

Public user profile only; no password or provider secrets.

- `id`, `first_name`, `last_name`, `created_at`, `updated_at`
- A user’s role is **not** global and therefore does not belong on this table.

#### `households`

- `id`, `name`, `created_at`, `updated_at`

#### `household_memberships`

Supports invitations, multiple households per user, and household-specific roles.

- `id`, `household_id`, `user_id`, `role` (`admin` or `member`), `created_at`, `updated_at`
- Unique `(household_id, user_id)`.
- Initially, admins manage household settings/invitations and can view all entries. Members manage their own entries and cannot view other members’ entries. All members may add entries and exchange rates.
- Only admins can edit or delete household-wide entries (`user_id = null`). “My data” excludes household-wide entries.

#### `household_invitations`

- `id`, `household_id`, `email`, `role`, `invited_by_user_id`, `token_hash`, `expires_at`, `accepted_at`, `created_at`
- Store only a hash of the invitation token. Invitations expire and can be accepted once.
- The accepting account’s normalized email must match the invitation email.

#### `currencies`

- `code` (primary key, for example `RSD`), `numeric_code`, `name`, `minor_units`
- `minor_units` controls storage conversion and display formatting; do not assume every currency has two decimal places.

#### `incomes` and `spendings`

Keep separate tables for the MVP because they have clear, simple semantics. Introduce a shared ledger only if later features justify it.

- `id`, `household_id`, `user_id` (nullable), `amount_minor`, `currency_code`, `entry_date`, `description` (nullable), `created_at`, `updated_at`
- `user_id` identifies the person whose income/spending it is; null means household-wide.
- `amount_minor > 0`; the table determines whether it is income or spending.
- Index `(household_id, entry_date)` and `(household_id, user_id, entry_date)`.
- Do not add `created_by_user_id` in the MVP. Attribution of who created a household-wide entry is deferred until there is a concrete need.

#### `exchange_rates`

- `id`, `household_id`, `from_currency_code`, `to_currency_code`, `rate`, `rate_date`, `is_monthly`, `created_at`, `updated_at`
- Example: `USD -> RSD`, `rate = 100` means 1 USD = 100 RSD.
- `rate` is an exact decimal with a documented precision/scale and must be positive.
- Initially use household-specific rates only; global/shared rates are post-MVP.
- Exact-date rates apply only on that exact date. Monthly rates apply to the calendar month containing the entry date; normalize their `rate_date` to the first day of that month. Rolling-window validity is post-MVP.
- Prefer exact-date rates over monthly rates. When both directions exist at the same scope/date, prefer the direct rate; otherwise infer the inverse as `1 / rate` without rounding the reciprocal prematurely.
- Prevent duplicate rates for the same household, pair, scope, and applicable date/month.
- Support direct and inverse conversion only; do not silently search arbitrary conversion paths.

#### `household_options`

One row per household.

- `household_id` (unique), `default_income_currency_code`, `default_spending_currency_code`, `default_display_currency_code`, timestamps

#### `user_options`

Overrides are household-specific because a user may belong to multiple households.

- `household_id`, `user_id`, nullable currency override fields, timestamps
- Unique `(household_id, user_id)`; null means “inherit the household default.”
- Store the active household separately as a session or user preference.

### Authentication tables

Keep authentication implementation replaceable:

- `auth_credentials`: `user_id`, normalized unique email, password hash, timestamps.
- `sessions`: opaque token hash, `user_id`, expiry, creation metadata; never store raw session tokens.
- A future `auth_identities` table can map external provider subjects to users without changing household/domain tables.

Use a maintained password-hashing implementation (Argon2id preferred), secure random tokens, HTTP-only/Secure/SameSite cookies, CSRF protection for cookie-authenticated mutations, login throttling, and generic credential-error messages.

## 5. Money and reporting rules

These rules need automated tests because subtle conversion bugs undermine all reports.

1. Parse user input as a decimal string and convert it to minor units using the currency exponent; never parse money into a float.
2. Preserve the original amount and currency on every entry.
3. Conversion happens at read/report time using the entry date, not the date the entry was created.
4. Same-currency conversion needs no exchange rate.
5. Use an exact-date rate before a monthly rate for the entry’s month. Use a direct rate before an inferred inverse when both are available at the same scope/date.
6. Convert using exact decimal arithmetic. Keep intermediate values exact and round only when producing an amount at the display currency’s minor-unit precision. Use **half-even** rounding, not half-up.
7. Normal formatting does not round to whole currency units: with EUR’s two minor units, €2.55 remains €2.55. Half-even affects only values requiring additional rounding; for example, at two decimals 2.545 becomes 2.54 and 2.555 becomes 2.56.
8. If any required rate is missing, the report must visibly identify the missing currencies/dates and mark totals incomplete. Never silently omit entries or treat them as zero.
9. Summary semantics are `net = income - spending`; “my” excludes household-wide entries.
10. Add a unique household/report display constraint so sums never mix currencies before conversion.

## 6. Application workflows

### Onboarding

1. A visitor creates an account with profile and login details.
2. The user creates a household; the same transaction creates the admin membership and default options.
3. The user selects or confirms default currencies.
4. An admin invites a user by email and assigns a role.
5. The recipient follows a single-use invitation link, signs in or registers, and accepts the invitation.
6. Members can add entries and rates and configure personal overrides.

### Daily entry

- Distinguish optional fields from required fields with defaults. Whenever a sensible default exists, display it preselected in the UI rather than hiding the field or requiring the user to choose it explicitly.
- Preselect the current local date, current user where applicable, and configured spending/income currency. These values remain visible and editable.
- Make adding an entry possible by entering only an amount and accepting the preselected values. Currency, date, and attribution are still required or deliberately household-wide—not optional; only description is optional.
- Return inline validation errors and preserve valid form input.
- Use HTMX to insert/update rows and refresh affected summaries without a full page load.
- Destructive actions require confirmation and provide clear success/error feedback.

### Reports

- Filters: household, date range, mine/all (where authorized), and display currency.
- Initial reports: total income, total spending, net, and a paginated entry list. Charts and category breakdowns can follow later.
- Show original and converted amounts where useful, plus warnings for incomplete conversion.

## 7. HTTP and API outline

HTML routes and JSON endpoints are separate adapters over the same application services; do not implement business rules twice.

### HTML (indicative)

- `/app`, `/app/entries`, `/app/reports`, `/app/settings`
- `/app/incomes/new`, `/app/spendings/new` and fragment endpoints for HTMX where needed
- `/login`, `/register`, `/logout`, `/invitations/{token}`

### JSON `/api/v1` (indicative)

- `GET/POST /households`
- `GET /households/{household_id}/members`
- `POST /households/{household_id}/invitations`
- `GET/POST /incomes`, `GET/PATCH/DELETE /incomes/{id}`
- `GET/POST /spendings`, `GET/PATCH/DELETE /spendings/{id}`
- `GET/POST /exchange-rates`, `PATCH/DELETE /exchange-rates/{id}`
- `GET/PUT /households/{household_id}/options`
- `GET/PUT /households/{household_id}/my-options`
- `GET /reports/summary?household_id=&from=&to=&currency=&scope=`
- `GET /currencies`

Use cursor or page-based pagination consistently, enforce maximum page sizes, validate date ranges, and return machine-readable error codes alongside human-readable messages. Final endpoint names can be adjusted when the first vertical slice is implemented.

## 8. Proposed repository layout

```text
<root>
├── backend/
│   ├── cmd/server/           # composition root and main()
│   ├── internal/
│   │   ├── auth/             # credentials, sessions, middleware
│   │   ├── households/       # memberships, invitations, options
│   │   ├── finance/          # entries, currencies, exchange rates
│   │   ├── reporting/        # conversion and summaries
│   │   ├── httpapi/          # shared HTTP concerns and JSON handlers
│   │   ├── web/              # HTML handlers and view models
│   │   ├── config/
│   │   └── store/            # DB pool/transactions and shared persistence
│   ├── db/
│   │   ├── migrations/
│   │   └── queries/
│   ├── web/
│   │   ├── templates/
│   │   └── static/
│   ├── api/openapi.yaml
│   └── go.mod
├── docs/
│   └── plans/
│       ├── README.md         # planning conventions and index
│       └── <timestamp>_<title>.md
├── mise.toml                 # tool versions and development tasks
├── compose.yaml              # local PostgreSQL only
├── AGENTS.md
└── README.md
```

Keep package boundaries based on behavior, not a large generic `models`/`utils` tree. Add infrastructure or dedicated clients at the repository root only when needed.

## 9. Delivery milestones

Each milestone should end in a runnable, tested vertical slice rather than a collection of disconnected layers. A milestone is a delivery checkpoint, not a single commit.

### Commit execution contract

- Deliver each milestone through small, coherent commits. Every commit must leave the application working, buildable, and testable; a milestone exit check does not replace per-commit validation.
- Order dependent commits as migration → model/query → application service → API handler → UI that consumes that API. Never reference a schema, service, or endpoint introduced only by a later commit.
- Server-rendered UI calls application services through HTML handlers and does not need to call the internal JSON API. Services must still precede their HTML handlers, and JSON endpoints must precede any UI that actually consumes them.
- Include relevant unit, integration, authorization, HTTP, and UI tests in the same commit as the behavior they cover. Later milestones may expand coverage, but testing must not be deferred to Milestone 5.
- Run each migration successfully and verify the application against both existing and newly introduced schema behavior before committing dependent code.
- Keep formatting, linting, documentation, and generated query updates synchronized with their related changes.
- Use `.agents/skills/commit-message/SKILL.md` to prepare each commit message from the staged changes. The surrounding instruction determines whether to commit immediately or request review first.

The sequences below are suggested commit boundaries, not a requirement to combine adjacent steps when smaller commits would be easier to review.

### Milestone 0 — Project bootstrap

- Record the confirmed product decisions and capture consequential architecture choices in short decision records as implementation proceeds.
- Create `mise` tasks for setup, run, test, lint, format, and database migration.
- Add local PostgreSQL, configuration, structured logging, health checks, migrations, and CI.
- Serve one server-rendered page and one health JSON endpoint.

Suggested commit sequence:

1. Add `mise` tooling, the Go module, and minimal development documentation.
2. Add a runnable server with configuration, structured logging, and a tested health endpoint.
3. Add Docker Compose PostgreSQL, migration tooling, and a database smoke test.
4. Add the server-rendered application shell and compiled Tailwind assets.
5. Add CI and verify all documented setup, test, lint, format, and migration tasks.

- **Exit:** a fresh checkout can start the app and database using documented commands; CI passes.

### Milestone 1 — Identity and household vertical slice

- Implement users, separated credentials/sessions, registration, login/logout, and auth middleware.
- Implement households, memberships, default options, and active-household selection.
- Add CSRF protection and authorization tests, including cross-household access attempts.
- Build responsive base layout, flash/form errors, and a simple household settings page.

Suggested commit sequence:

1. Add the shared responsive layout and form/error rendering primitives.
2. Add the identity/auth schema migration.
3. Add auth persistence and credential/session services with tests.
4. Add registration/session HTTP handlers with CSRF protection and tests, then their server-rendered pages.
5. Add the household/membership/options schema migration.
6. Add household persistence and services with authorization tests.
7. Add household JSON endpoints with contract and cross-household access tests.
8. Add household settings and active-household HTML workflows using the existing services.

- **Exit:** a user can securely register, create a household, sign out/in, and access only authorized household data.

### Milestone 2 — Income and spending

- Seed currencies and implement amount parsing/formatting.
- Add income/spending CRUD services, queries, HTML forms/lists, and JSON endpoints.
- Add HTMX create/edit/delete interactions with non-JS form fallbacks where practical.
- Add filtering, pagination, empty states, and ownership/authorization checks.

Suggested commit sequence:

1. Add the currency schema/seed migration, then money parsing and formatting with unit tests.
2. Add the income/spending schema migration.
3. Add entry queries/persistence with PostgreSQL integration tests.
4. Add entry application services with ownership, household authorization, and validation tests.
5. Add entry JSON endpoints with contract tests.
6. Add server-rendered entry forms and lists using the existing services.
7. Add HTMX create/edit/delete enhancements with fragment and non-JS fallback tests.
8. Add filtering and pagination to the query and service layers with tests.
9. Extend entry JSON endpoints with filtering/pagination contract tests.
10. Add the corresponding filter controls, pagination, and empty states to the UI.

- **Exit:** personal and household-wide entries can be managed on mobile and desktop without mixing households or users.

### Milestone 3 — Exchange rates and summaries

- Add exchange-rate CRUD and precedence/uniqueness rules.
- Implement decimal conversion, rounding, and missing-rate reporting with table-driven tests.
- Add mine/all summary endpoints and report UI with date/currency filters.

Suggested commit sequence:

1. Add the exchange-rate schema migration.
2. Add exchange-rate persistence and services with precedence/uniqueness tests.
3. Add exchange-rate JSON endpoints with contract and authorization tests.
4. Add the server-rendered exchange-rate CRUD UI using the existing services.
5. Add decimal conversion and reporting services with table-driven tests for direct/inverse rates, rounding, dates, and missing rates.
6. Add summary JSON endpoints with mine/all authorization and contract tests.
7. Add the report HTML and HTMX filters using the existing reporting services.

- **Exit:** a multi-currency monthly summary is correct, explainable, and visibly incomplete when rates are unavailable.

### Milestone 4 — Invitations, preferences, and UX polish

- Implement secure invitation creation/acceptance and membership management basics.
- Add household defaults and per-user overrides.
- Improve accessibility, keyboard behavior, responsive layouts, loading/error states, and empty states.
- Optionally add a web manifest/service worker only after the core responsive app works well; do not make offline support an MVP promise.

Suggested commit sequence:

1. Add the invitation schema migration.
2. Add invitation persistence and services with token, expiry, email-match, and authorization tests.
3. Add invitation JSON endpoints with contract tests.
4. Add server-rendered invitation and membership workflows using the existing services.
5. Add any required user-options schema migration.
6. Add options persistence and preference services with inheritance tests.
7. Add options JSON endpoints with tests.
8. Add household/user preference UI using the existing services.
9. Apply accessibility and responsive-UX polish in focused commits with relevant browser coverage.
10. Add optional PWA behavior as a separate commit only after the core workflows pass their smoke tests.

- **Exit:** the complete onboarding-to-report workflow works for two users with different roles and preferences.

### Milestone 5 — Local quality and handoff

- Add security headers, request timeouts, graceful shutdown, rate limiting where appropriate, and log redaction.
- Run the complete integration, authorization, and browser smoke suites; fill remaining coverage gaps without replacing tests that should accompany earlier commits. Document known limitations.
- Document local setup and operation using `mise` for Go/tooling and Docker Compose for PostgreSQL. Deployment, hosting, and production operations are outside this phase.

Suggested commit sequence:

1. Add each runtime/security hardening concern in a focused commit with relevant tests.
2. Add any missing end-to-end smoke coverage and run the complete validation suite.
3. Finalize local development documentation and known limitations.

- **Exit:** local setup, testing, operation, and MVP acceptance checks are documented and repeatable.

### Post-MVP candidates

Prioritize based on actual use: categories/tags, budgets, recurring entries, global/shared or imported exchange rates, rolling-window rates, creator attribution for shared entries, read-only MCP tools, then carefully confirmed MCP writes. Native apps, advanced permissions, audit history, and deployment automation should follow demonstrated need.

## 10. Testing and quality strategy

- Unit-test money parsing, currency exponents, conversion precedence, rounding, date boundaries, and report arithmetic.
- Test services with real PostgreSQL for constraints, transactions, and query behavior; avoid relying only on mocks.
- Test HTTP status codes, validation errors, sessions, CSRF, HTMX fragment/full-page responses, and JSON contracts.
- For every household resource, test that another household’s ID cannot be read or modified.
- Add a small browser smoke suite for register → household → entry → rate → report and invitation acceptance.
- Run formatting, static analysis/linting, unit tests, and integration tests in CI.
- Seed useful development data, but keep test fixtures independent and deterministic.

### MVP acceptance checklist

- Two users can belong to a household with enforced admin/member permissions.
- Users cannot access another household’s entries, settings, invitations, rates, or reports.
- Entries retain exact amounts and support the configured currency defaults.
- Reports correctly handle same-currency, direct, inferred-inverse, exact-date, monthly-rate, rounding, and missing-rate cases.
- Core workflows are usable on a narrow mobile viewport and with keyboard navigation.
- The API and HTML UI produce the same results because they use the same services.
- Documentation allows a new developer to set up, test, and run the project from scratch.

## 11. Confirmed product decisions

1. **Membership:** A user can belong to multiple households; membership and role are household-specific.
2. **Visibility:** Only admins can view all household entries and household-wide reports. Members see and manage their own entries.
3. **Shared entries:** Only admins can edit entries with `user_id = null`; “my data” excludes them. Tracking who created such entries is postponed.
4. **Rate ownership:** Exchange rates are household-specific in the MVP. Global/shared rates may be introduced later.
5. **Rate validity:** Exact-date rates apply only on that date; rolling-window rates are postponed. Direct inverse rates are inferred, with no chained conversion.
6. **Rounding:** Preserve normal currency minor units and use half-even rather than half-up when conversion requires rounding. For example, €2.55 remains €2.55 at EUR’s normal two-decimal precision.
7. **Login identity:** Use email only, normalized and unique.
8. **Invitations:** The accepting account’s normalized email must match the invitation email.
9. **Deletion:** Hard deletion is acceptable for the MVP; audit history and soft deletion are deferred.
10. **Local environment:** Skip deployment in this phase. Run PostgreSQL in a Docker container through Compose and manage Go and development tasks with `mise`.

Begin Milestone 0 with these decisions as the baseline. Avoid designing post-MVP features in detail before the first end-to-end workflow is usable.

## 12. Implementation status

| Milestone | Status |
| --- | --- |
| 0 — Project bootstrap | Complete on 2026-11-08 |
| 1 — Identity and household vertical slice | Complete on 2026-11-08 |
| 2 — Income and spending | Complete on 2026-11-08 |
| 3 — Exchange rates and summaries | Complete on 2026-11-08 |
| 4 — Invitations, preferences, and UX polish | Complete on 2026-11-08 |
| 5 — Local quality and handoff | Not started |

Milestone 0 provides the mise task runner, Go module, local PostgreSQL
Compose service, embedded SQL migration runner, JSON health endpoints,
server-rendered application shell, build-time Tailwind CSS, structured
logging, and CI. The local setup and check commands are documented in the
repository README.

Milestone 1 adds Argon2id credentials, revocable database sessions,
registration/login/logout with CSRF protection, household memberships and
roles, active-household selection, admin settings, and household JSON
endpoints. Household currency defaults were stored as RSD text codes in this
milestone; Milestone 2 added the currencies table and foreign keys.

Milestone 2 adds seeded currencies, exact decimal amount parsing and display,
separate income and spending tables, entry services with owner/admin
authorization, JSON CRUD endpoints, responsive entry forms and lists, filters,
pagination, empty states, and progressive HTMX updates. Household-wide entries
are admin-only to create, view, edit, and delete; members manage personal
entries only. See ADR 0006 for the full attribution rules.

Milestone 3 adds household exact-date and monthly exchange rates, direct and
inferred-inverse resolution with no chaining, exact rational conversion with
half-even display rounding, mine/all summaries, and report warnings that name
missing currency pairs and entry dates. Rate management, summary JSON, rate
management HTML, and report HTML are covered by unit and integration tests.
See ADR 0007.

Milestone 4 adds secure single-use invitation links, public invitation
acceptance with email matching and safe post-login redirects, admin
invitation management, household currency-default settings, per-user currency
inheritance, preference API and UI, mobile navigation, focus styling, and
reduced-motion support. See ADR 0008.
