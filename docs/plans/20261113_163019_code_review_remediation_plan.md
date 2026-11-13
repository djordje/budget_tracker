# Code Review Remediation

## 1. Purpose and context

This plan remediates the implementation produced by the [Budget Tracker MVP plan](./20261108_201136_budget_tracker_mvp_plan.md). It does not redefine the product or replace that plan. The original MVP plan is the immutable initial-implementation baseline; do not edit it while executing this remediation.

The reviewed implementation has not been pushed and has no production data. Rewrite the existing milestone branches in place and fold corrections into the commits that introduced the affected behavior. Add or split commits only where that makes the resulting history clearer. Original commit IDs below identify the pre-remediation commits; those IDs will change during rebasing.

The current linear branch checkpoints are:

- `main` — planning baseline.
- `milestone-0` — project bootstrap.
- `milestone-1` — identity and households.
- `milestone-2` — income and spending.
- `milestone-3` — exchange rates and reports.
- `milestone-4` — invitations, preferences, and UX.
- `milestone-5` — quality and handoff.

Start each milestone on its correspondingly named branch, preserve the dependency order between milestones, and finish with a buildable, tested commit at every step.

### Review coverage

This plan consolidates the findings from the temporary manual and automated code-review fragments used during planning. Those fragments are not retained documentation; this plan is the self-contained record of the resulting decisions and remediation work.

Where reviews overlap, implement one coherent fix rather than separate patches. In particular, invitation revocation needs both nested-household binding and acceptance/revocation race handling; session primary-key removal must be coordinated with household activation; and template extraction must preserve the web handler and HTMX fixes.

## 2. Goals

- Correct the confirmed security, correctness, transaction, concurrency, and error-reporting defects.
- Make configuration, logging, JSON handling, HTML errors, and component rendering consistent.
- Simplify primary keys and propagate identifier changes through migrations, services, both HTTP adapters, and tests.
- Define explicit transactional semantics for household creation, preferences, and reports.
- Bound and serialize login-throttle state and provide expired-session cleanup.
- Add focused unit tests at adapter boundaries while retaining real-PostgreSQL integration and authorization tests.
- Centralize repeated template styling and genuinely shared structure without introducing a frontend or component framework.
- Produce a clean, logical rewritten history whose commit messages accurately describe each amended commit.

## 3. Confirmed decisions

### Runtime and history

1. `DATABASE_URL` is required in every environment. `.env.example`, loaded by `mise`, documents the local Compose URL; the Go configuration package does not contain a fallback DSN.
2. `APP_ENV` accepts only `development` or `production`. Invalid values are startup errors, not implicit development mode.
3. Set the configured application logger with `slog.SetDefault` so dependency-injected and fallback logging use the same format and metadata.
4. Revise migrations 0002–0008 in place. Do not add compatibility migrations for converting the old UUID schema. Recreate disposable local/test databases rather than applying edited migrations over an already-migrated database.
5. Keep the existing milestone order and branch names. Fold fixes into the originating commits wherever practical.

### Primary keys

| Table | Target key |
| --- | --- |
| `users`, `households`, `household_invitations` | Keep UUID v4 primary keys. |
| `auth_credentials`, `household_options` | Keep the existing one-to-one UUID primary/foreign keys. |
| `sessions` | Drop the UUID `id`; use `token_hash` as the primary key. |
| `household_memberships` | Drop the UUID `id`; use `(household_id, user_id)` as the primary key. |
| `incomes`, `spendings`, `exchange_rates` | Use `bigint GENERATED ALWAYS AS IDENTITY`; retain existing uniqueness rules. |
| `currencies`, `user_options` | Keep the existing natural/composite primary keys. |

Use `int64` for entry and exchange-rate IDs in Go. Parse route IDs as positive base-10 integers and reject zero, negative, malformed, and overflowing values. Keep JSON ID fields as decimal strings so the transport representation remains a string. Do not change users, households, or invitations to integer IDs, and do not adopt UUID v7.

Removing `sessions.id` requires replacing internal session-addressing uses, including active-household updates, with the stored token hash or another safe token-hash-derived identifier. Never substitute the raw session token for persistence or logging.

### Transactions and consistency

1. Household creation and activation are two database operations in one required application-service workflow and one PostgreSQL transaction:
   - insert the household, initial admin membership, and default options;
   - set the current session's active household;
   - commit only if every operation succeeds.
2. Both HTML and JSON handlers call that workflow. An activation failure rolls back creation and returns an error; neither adapter reports success. A retry after rollback must not duplicate a partially created household.
3. Personal overrides and resolved effective options come from one service operation with one consistent database view. Preference updates return the values committed by the same service workflow rather than performing a separate handler-level read.
4. A report summary represents one database snapshot. Use a read-only `REPEATABLE READ` transaction covering display options, currencies, entries, and exchange rates. Make the finance/reporting reads participate in that transaction instead of issuing independent pool reads.

### Authentication maintenance and throttling

1. Add explicit expired-session deletion using `sessions_expires_at_idx`. Invoke it opportunistically after successful registration/login at a documented bounded frequency, at most once per hour per process. A cleanup failure is logged and does not invalidate a session that was already created successfully. Do not add a background worker or run an unbounded cleanup on every request.
2. Replace limiter `Allowed` followed by `RecordFailure` with one mutex-protected admission/reservation operation. Every admitted login attempt consumes capacity immediately; successful login resets the key; transient authentication/service errors leave the reservation in place. Remove `RecordFailure` if no separate use remains.
3. Prune idle keys globally during bounded-frequency limiter maintenance, enforce a documented hard cap with an explicit eviction policy, and use bounded fixed-size keys so arbitrary email input is not retained. Preserve the five-attempt/fifteen-minute policy and the documented single-instance limitation.

### Review scope

Include the low-priority and optional maintainability work identified in the reviews: consistent options reads, focused HTTP handler tests, JSON/helper file organization, rendering and authentication/policy deduplication, and the no-visual-change template design-system refactor.

For anonymous HTML access, `RequireAuth` owns ordinary `/login` redirects. Invitation acceptance is the explicit exception: retain its invitation-aware login redirect with a safe return path, and remove route/middleware duplication that makes that branch unreachable. Never replace service authorization with template visibility or handler prechecks.

## 4. Non-goals

- Do not edit the original MVP plan or change confirmed product, money, exchange-rate, attribution, or authorization rules.
- Do not add deployment support, a distributed limiter, a background session-cleaning service, or multi-instance coordination.
- Do not add deployment-dependent `__Host-` CSRF cookies or Origin validation unless the single-host threat model changes.
- Do not expand panic recovery into a redesign of ServeMux-generated API 404/405 responses.
- Do not introduce an HTTP framework, SPA, template engine replacement, generic component framework, unsafe `template.HTML` slot system, or large string-keyed template `dict` API.
- Do not perform the visual redesign. The template work preserves appearance while centralizing the existing design.
- Do not pursue arbitrary coverage targets or replace PostgreSQL integration tests with mocks.
- Do not preserve the unused UUID fields merely to avoid updating fixtures or handlers.

## 5. Rewriting and commit execution contract

### Preserve and rewrite branch checkpoints

This plan and its index entry are committed immediately after the original MVP plan and before Milestone 0. That documentation commit is the base for the remediated implementation history, so this plan remains available on every milestone branch. The temporary code-review fragments are not included in that commit or any other commit.

Before rewriting, record the original tips with temporary local refs, for example `pre-review/milestone-0` through `pre-review/milestone-5`. Keep the tracked working tree clean and keep temporary review fragments untracked.

For each milestone in numeric order:

1. Switch to `milestone-N`.
2. Rebase it onto the already-remediated previous milestone, using the saved `pre-review/milestone-(N-1)` ref as the old upstream. For Milestone 0, rebase onto `main`.
3. Mark commits for editing, apply the listed remediation at the originating commit, and validate that commit before continuing.
4. Move the milestone branch only through the rebase; do not merge old and new histories or create a parallel remediation branch.
5. At the milestone exit, run the complete validation suite and confirm that the next milestone still replays onto this history.

Delete the temporary `pre-review/*` refs only after the final rewritten history and all checks have been accepted.

### Every fold requires a commit-message review

A fold is any amendment of remediation changes into an existing commit, including repeated amendments to the same commit.

1. Stage only the changes intended for that commit; the commit-message skill must not stage files automatically.
2. Follow `.agents/skills/commit-message/SKILL.md`: inspect `git status --short`, `git diff --cached --stat`, and the complete cached diff.
3. Regenerate or verify the **entire** Conventional Commit message against the combined original plus staged changes. Do not assume the old subject remains accurate and do not use `git commit --amend --no-edit` for a remediation fold.
4. Amend with the reviewed message, continue the rebase, and run the required checks. Do not request commit approval unless the surrounding instruction requires it.
5. Review only staged changes for the message; do not describe unrelated unstaged or untracked files.

When a concern is split into a new commit, prepare its message through the same skill. Keep migration → persistence/service → API → dependent UI ordering valid at every commit. Tests and documentation directly related to a behavior belong in the same fold, not in a final test-only catch-up commit.

### Validation levels

Use the checks relevant to each fold and report exactly what ran:

- Go changes: `mise run format`, focused unit/race tests, then `mise run check`.
- Service, schema, transaction, route, or authorization changes: also `mise run test:integration` against real PostgreSQL.
- Template/static changes: `mise run assets:build`, rendering/handler tests, and relevant HTML/HTMX integration or smoke tests.
- Migration edits: recreate a disposable database with `mise run db:reset`, verify fresh migration and upgrade behavior, and never run the reset against data that must be retained.
- Every milestone exit and final handoff: `mise run check:all`.
- Documentation-only commits: review content and relative links; application tests are not required.

A later milestone's full suite does not retroactively validate an earlier amended commit.

## 6. Milestone 0 — Project bootstrap

**Branch:** `milestone-0`
**Review themes:** configuration, global logging, panic responses, migration preflight, static files, health constructor/test organization.

### Commit sequence

1. **`4d57945` — `chore: bootstrap go module and mise tooling`**
   - Keep the bootstrap intact. Confirm `.env.example` and `mise` setup provide the local database URL needed by the next commit.
   - No review fold is required unless later configuration changes reveal stale setup documentation.

2. **`7474890` — `feat: add server health checks and local postgres`**
   - Require `DATABASE_URL` unconditionally; remove `DefaultDatabaseURL` and the production-only branch.
   - Validate `APP_ENV`, use the default-string helper consistently for `HTTP_ADDR` and `LOG_LEVEL`, remove the unreachable log-level guard, leave `WEB_STATIC_DIR` optionally empty, and unexport the remaining HTTP default.
   - Update configuration tests, `.env.example` usage, and README setup/run instructions. Bare `go run` without configuration must fail clearly; documented `mise` setup remains the supported path.
   - Call `slog.SetDefault(logger)` immediately after `logging.New` and test the selected behavior without introducing global-test races.
   - Make `Recoverer` API- and commitment-aware: before commitment, `/api/v1` panics use status 500 and the standard `internal_error` JSON envelope; after commitment, log only and append nothing. Preserve non-API behavior, request IDs, and stack logging.
   - Give `NewHealth` the standard nil-logger fallback and move health-specific tests to `health_test.go` while touching this area.
   - Tests: configuration defaults/errors, invalid environment, JSON/text logging fallback, API and HTML panic before commitment, no second response after commitment, and health behavior.

3. **`7dfbca2` — `feat: add postgres migration tooling`**
   - Split migration processing into complete preflight and application phases.
   - Before applying anything, validate all missing files and applied checksums, determine the greatest applied version, and reject every pending lower-version/backfilled migration.
   - Continue applying valid pending versions in increasing order with the existing advisory lock, per-migration transaction, and partial-result behavior for application-phase failures.
   - PostgreSQL tests: versions 1 and 3 applied followed by pending 2; changed applied checksum plus another pending migration; fresh installation; ordinary increasing upgrades. In both invalid-history cases, assert that no pending migration ran or was recorded.

4. **`06efac9` — `feat: add server-rendered application shell`**
   - No independent review correction. Preserve the shell and build-time Tailwind setup.

5. **`0e94020` — `ci: add continuous integration`**
   - Fix `Static` after prefix stripping: reject empty/directory/trailing-slash paths and any path segment beginning with `.`, including the first segment and nested segments.
   - Add root-level and nested dotfile regression tests using a configured static directory and confirm ordinary assets such as `css/app.css` still render.
   - Keep CI, configuration documentation, and setup checks synchronized with the earlier folds.

**Exit:** configuration fails safely, all logging uses the application logger, migration history is fully validated before writes, static dotfiles are inaccessible, and a fresh configured checkout passes CI-equivalent checks.

## 7. Milestone 1 — Identity and household vertical slice

**Branch:** `milestone-1`
**Review themes:** session/membership keys, registration validation, expired sessions, atomic household activation, JSON transport, dead auth view state.

### Commit sequence

1. **`f1bd773` — `feat: add identity and session schema`**
   - Revise migration 0002 so `sessions.token_hash` is the primary key and the UUID `id` column is absent.
   - Keep `users` and `auth_credentials` unchanged and retain the expiry index used by cleanup.
   - Validate a fresh migration before dependent service code.

2. **`99a15cd` — `feat: add password and session services`**
   - Remove session UUID scans/model fields and address sessions by token hash throughout persistence and middleware-facing models.
   - Reject invalid UTF-8 and NUL in registration names before persistence; apply the same control-character policy as household names while preserving trimming and character-count limits.
   - Add `DeleteExpiredSessions` with `WHERE expires_at <= now()`, then invoke it through a process-local, concurrency-safe once-per-hour opportunistic cleanup after successful registration/login. Log cleanup failure without turning successful authentication into a failure.
   - Tests: token-hash identity, expired-session deletion without presenting each token, valid-session preservation, cleanup frequency/concurrency, registration Unicode/whitespace/control/NUL/invalid-UTF-8 cases, and adapter validation mapping.

3. **`3ed5cfd` — `feat: add session cookies and csrf protection`**
   - Adapt middleware tests to token-hash session identity. Preserve the accepted double-submit CSRF design.
   - Move `TestWriteValidationError` to the consolidated JSON tests when `decode.go` is introduced in commit 7.

4. **`2026e7b` — `feat: add registration login and logout pages`**
   - Remove unused `registerPageData.FormError`, `loginPageData.FieldErrors`, and unreachable login field-error template branches.
   - Preserve submitted values and actual form-level login errors. Do not remove intentional invitation return-path handling.

5. **`53d56fb` — `feat: add household and membership schema`**
   - Revise migration 0003 so `household_memberships` has composite primary key `(household_id, user_id)` and no UUID `id`.
   - Keep `households` and `household_options` keys unchanged and validate the fresh schema.

6. **`7490c52` — `feat: add household membership services`**
   - Remove membership-ID persistence/scans and update fixtures.
   - Refactor active-session updates to use token-hash identity after the Milestone 1 schema change.
   - Replace standalone household creation with the confirmed create-and-activate application-service workflow. Use one transaction for household, membership, options, and current-session activation; distinguish an invalid/missing session from infrastructure errors.
   - Tests: successful activation, activation failure rolls back all inserted rows, retry after rollback creates only one household, cross-user/session rejection, existing membership authorization, and cross-household access.

7. **`01fa418` — `feat: add household json endpoints`**
   - Update creation to call the atomic service workflow and remove the handler's best-effort session call/warning. Return `201` and `Location` only after commit.
   - Consolidate `decodeJSON` with JSON writing/errors in `json.go`; delete `decode.go` and create `json_test.go`, including the moved validation-envelope test. Keep decoding behavior unchanged in this fold.
   - Add small household service stubs for path parsing, authentication prerequisites, service-error mapping, activation failure, response envelopes, and `Location`; retain full-stack transaction tests.
   - Test the preserved JSON behavior and file organization before tightening its public contract in the next commit.

8. **New commit — `fix: tighten json request decoding`**
   - Parse `Content-Type` with `mime.ParseMediaType`; accept only supported JSON media types and valid parameters. Require exactly one top-level object; reject `null`, arrays, scalars, empty/malformed bodies, unknown fields, and trailing values while allowing trailing whitespace.
   - Use shared typed decode errors to map malformed input to 400, bodies over 1 MiB to 413, and unsupported/lookalike media types to 415, all with safe JSON error envelopes. Keep the body limit and avoid parser-detail leakage.
   - Table-test JSON media types, body shapes, unknown/trailing content, exact/over-limit bodies, statuses, error codes, and content type. Update all existing JSON handlers to map the shared errors consistently.

9. **`9d859a3` — `feat: add household dashboard and settings`**
   - Use the same atomic creation workflow in HTML and render an error instead of logging activation failure and redirecting with success.
   - Add HTML regression coverage for success, validation, and activation failure; verify failed creation leaves no household behind.
   - Keep ordinary authentication redirects in `RequireAuth`; broader duplicate-branch removal is completed after all protected handlers exist.

10. **`8b5101a` — `docs: document identity and household milestone`**
   - Update relevant authentication/session ADR and local documentation for token-hash session identity, required atomic activation, and cleanup semantics.
   - Do not revise the original MVP plan beyond preserving its already-existing historical text.

**Exit:** session and membership keys match the target schema, expired sessions are cleaned up, invalid text becomes validation feedback, JSON decoding has a precise contract, and household creation cannot report success without activation.

## 8. Milestone 2 — Income and spending

**Branch:** `milestone-2`
**Review themes:** integer entry IDs, pagination and text validation, authorization policy ownership, web cleanup, HTMX deletion errors, rendering deduplication.

### Commit sequence

1. **`49909c4` — `feat: add currencies and exact money parsing`**
   - No review correction. Preserve currency exponents and exact money tests.

2. **`9c80396` — `feat: add income and spending schema`**
   - Revise migration 0005 to use `bigint GENERATED ALWAYS AS IDENTITY` for both entry tables.
   - Keep household/user foreign keys, positive amounts, and existing indexes unchanged; validate fresh migration.

3. **`d530a8a` — `feat: add entry services and authorization`**
   - Convert entry IDs and parameters/scans to `int64` and update all PostgreSQL fixtures.
   - Check pagination multiplication before calculating the offset; return field validation for unrepresentable offsets while retaining defaults, page-size limits, and empty out-of-range pages.
   - Reject invalid UTF-8 and NUL in descriptions before writes. Preserve intentional newlines/tabs and existing trim/length behavior.
   - Make finance own one entry-modification policy used by both service enforcement and web visibility metadata. Remove duplicate web update-time authorization decisions rather than weakening service checks.
   - Tests: maximum-integer and boundary page/offset combinations, description text cases, positive IDs, ownership/admin rules, and cross-household not-found behavior.

4. **`ac390ae` — `feat: add entry api and management ui`**
   - Parse web/API path IDs with positive `strconv.ParseInt` semantics and format external IDs as decimal strings. Reject malformed, zero, negative, and overflowing IDs without invoking services.
   - Remove unused `entryListPageData.PageSize` and `entryFormPageData.Kind`; use the finance-owned modification policy for button/edit visibility and rely on services for mutation authorization.
   - Add focused `httpapi` entry tests for authentication/active household prerequisites, dates, pagination, scope/currency/household query parsing, validation/forbidden/not-found/conflict/internal mapping, envelopes, nullable descriptions, formatted amounts, and `Location`.
   - Expand web regression coverage for submitted-value preservation, no-active-household redirects, full-page validation, ownership/admin behavior, and cross-household IDs.

5. **`f839339` — `feat: add htmx entry interactions`**
   - Separate the entry-list return URL from the success-notice argument. On delete failure, never set a redirect containing `notice=entry_deleted` and never return a false-success 204.
   - Return an HTMX-compatible error fragment/response with the original 403, 404, or 500 status and useful feedback compatible with the delete form's select/target/swap contract. Preserve existing full-page errors and successful-delete redirects.
   - Consolidate `render` and `renderFragment` around one buffered template-execution/response helper. Preserve clean 500 handling on execution failure and intentional status-200 HTMX entry-validation swaps.
   - Remove startup-time `Handler.year`; compute the year in `basePageData` so it does not become stale.
   - Tests: HTMX delete success and forbidden/missing/internal failures with no deletion notice, fragment selector compatibility, validation fragments, successful redirects, buffering/errors, and ordinary full-page behavior.

6. **`0840c3a` — `docs: document income and spending milestone`**
   - Update implementation documentation and fixtures for decimal-string integer IDs where needed. Preserve ADR 0006's attribution policy.

**Exit:** entry persistence and both adapters use integer IDs safely, invalid pagination/text is handled before database errors, failed HTMX deletes look like failures, and rendering/authorization behavior has one authoritative implementation.

## 9. Milestone 3 — Exchange rates and summaries

**Branch:** `milestone-3`
**Review themes:** integer rate IDs, report snapshot consistency, rate/report view cleanup and errors, HTTP adapter tests.

### Commit sequence

1. **`9e5cbd3` — `feat: add exchange rate schema`**
   - Revise migration 0006 to use a `bigint` identity primary key.
   - Keep exact/monthly partial uniqueness indexes and exact decimal representation unchanged; validate fresh migration.

2. **`e21b19e` — `feat: add exchange rate services`**
   - Convert exchange-rate IDs, scans, parameters, and fixtures to `int64` without changing rate precedence or uniqueness behavior.
   - Retain authorization integration coverage and add identifier boundary cases.

3. **`3001839` — `feat: add reporting conversion and summaries`**
   - Execute the complete summary read workflow in a read-only `REPEATABLE READ` transaction. Refactor finance read methods to accept/participate in that transaction while keeping authorization in services.
   - Include display-option lookup, currency metadata, entries, and rates in the same snapshot; preserve exact rational conversion, half-even final rounding, direct/inverse precedence, and missing-rate reporting.
   - Add a coordinated PostgreSQL concurrency test proving that concurrent entry/rate/option writes cannot produce a mixed-statement summary, alongside existing table-driven conversion tests.

4. **`b882615` — `feat: add exchange rate and report ui`**
   - Update JSON/HTML rate routes and response IDs to positive decimal `int64` values, with malformed/overflow/cross-household tests.
   - Replace `rateInputValue(rate.Rate)` with `rate.RateString()` and remove the duplicate helper and unused `exchangeRateFormPageData.ID`.
   - Remove the impossible `errNoActiveHousehold` check in report page setup; retain the actual `ActiveHousehold == nil` redirect.
   - Route report failures through report-specific status/copy and rate failures through rate-specific handling. Never render “Rate not found” for household/options failures; add not-found and forbidden tests.
   - Add focused exchange-rate and report API stub tests for path/query translation, service errors, envelopes, formatted rates/amounts, missing-rate data, and mine/all authorization mapping. Keep integration tests authoritative for real authorization.

5. **`fa5a883` — `docs: document exchange rates and reports`**
   - Update ADR 0007/reporting documentation with the confirmed single-snapshot contract and integer rate IDs. Do not alter conversion semantics.

**Exit:** rates use the target key strategy, reports have documented and tested snapshot semantics, and rate/report adapters use correct IDs and resource-specific errors.

## 10. Milestone 4 — Invitations, preferences, and template reuse

**Branch:** `milestone-4`
**Review themes:** invitation binding/races, consistent options, web cleanup, page-specific errors, semantic CSS and structural components.

### Commit sequence

1. **`69b15b6` — `feat: add household invitation schema`**
   - Keep invitation UUIDs and token hashes unchanged. No independent schema correction is required.

2. **`4abba87` — `feat: add household invitation services`**
   - Change revocation to accept actor, path household ID, and invitation ID. Authorize the actor against the path household and operate on the matching pair; a mismatched pair returns not found without revealing the actual household.
   - Make revocation state-aware with a transaction/row lock or `DELETE ... WHERE id = ... AND household_id = ... AND accepted_at IS NULL` plus rows-affected/state handling. Preserve a clear conflict when acceptance wins and not-found when revocation has already removed the invitation.
   - Coordinate acceptance and revocation in real-PostgreSQL tests for both orderings: accepted invitations are not deleted, and successful revocation prevents membership creation.
   - Retain member/outsider/accepted/cross-household tests and admin-only authorization.

3. **`44d2cec` — `feat: add user currency option schema`**
   - Keep the composite primary key unchanged.

4. **`ed7a4e3` — `fix: redact invitation tokens from request logs`**
   - Preserve token redaction and its tests while rebasing middleware changes from Milestone 0.

5. **`5982283` — `feat: add household currency preference services`**
   - Add one service result/operation containing personal overrides and effective options, loaded from one consistent query/view with membership authorization.
   - Change personal-option updates to perform mutation and read-your-write result assembly in one service transaction; return only after commit and never leave handlers to issue a second effective-options read.
   - Keep household defaults available to the preferences view without duplicating inheritance rules.
   - Ensure the new reads participate correctly with the reporting transaction introduced in Milestone 3.
   - Tests: inherited/overridden values together, update responses matching committed values, service failures, concurrent consistency semantics, and existing authorization.

6. **`e69afba` — `feat: add invitation and preference workflows`**
   - Parse and pass both path IDs in JSON and HTML revocation. Reject malformed household IDs before service invocation, return 404 for mismatched pairs, leave invitations intact, and keep outsider/member/admin integration coverage.
   - Use the combined options operation in `MyOptions` and `UpdateMyOptions`; update HTML preference loading to use the same service semantics.
   - Remove `invitationPageData.LoggedIn` in favor of embedded `Authenticated`, remove the always-nil `preferencesData` error parameter, and route preference/options errors through household/preferences-specific copy rather than `renderRateError`.
   - Preserve the invitation-aware anonymous redirect by making invitation acceptance the documented exception to ordinary `RequireAuth` handling; test safe post-login return behavior.
   - Add invitation/options API unit tests for path decoding, nested binding, validation/conflict/not-found/forbidden/internal mapping, nullable fields, timestamps, and response envelopes.

7. **`dfdf2f4` — `feat: improve accessibility and mobile navigation`**
   - Preserve the existing accessibility/mobile improvements.
   - Add semantic `@layer components` primitives for repeated buttons, controls, labels, errors, cards, headings, and alerts using the current colors, spacing, radii, typography, focus, and responsive behavior.
   - Convert existing templates to complete, statically discoverable classes without intentionally changing appearance or behavior. Build Tailwind and test the affected pages.

8. **New commit — `refactor: extract shared ui template components`**
   - Add flat `_ui.html`/`_forms.html` partials for genuinely shared alerts, page headers, field errors, empty states, pagination, and navigation destinations where final markup is actually common.
   - Prefer small typed view models. Standardize deterministic error IDs, `aria-invalid`, and `aria-describedby` while retaining input-specific attributes, submitted values, CSRF fields, destructive confirmations, and responsive layout differences.
   - Keep template definition names unique and avoid unsafe rendered-HTML slots or a general `dict` interface.
   - Test component output, accessibility associations, validation values, full pages, and HTMX fragments; run the asset build.

9. **New commit — `refactor: share currency and exchange rate forms`**
   - Extract one exchange-rate add/edit form and shared currency selectors with typed inputs and correct selected-value behavior.
   - Reuse navigation or other domain structure only where it remains clear and genuinely shared.
   - Preserve form actions, outer IDs, `hx-target`, `hx-select`, `hx-swap`, validation output, non-JS fallbacks, and authorization-related visibility.
   - Cover both add/edit pages and HTMX responses and run the complete relevant template/integration checks.

10. **`8807e0e` — `docs: document invitations and currency preferences`**
    - Update ADR 0008 and UI documentation for nested revocation, consistent preferences, and the reusable no-visual-change design primitives.

**Exit:** invitation revocation is nested and race-safe, preference responses are internally consistent, and pages share design primitives/components without changing product behavior or intentionally redesigning the UI.

## 11. Milestone 5 — Local quality and handoff

**Branch:** `milestone-5`
**Review themes:** bounded atomic login throttling, remaining helper/auth deduplication, complete regression coverage, operations documentation.

### Commit sequence

1. **`673a95a` — `feat: harden http responses and login throttling`**
   - Replace check-then-record limiting with atomic `TryAcquire`-style admission. Reserve before password verification, reset on success, retain reservations after transient errors, and remove obsolete failure-recording calls.
   - Add global idle-key maintenance, a documented total-state hard cap and eviction policy, and fixed-size bounded keys (for example, a hash of normalized host/email material) so arbitrary input is not retained.
   - Preserve `Retry-After`, five attempts/fifteen minutes, generic credential errors, and single-instance limitations.
   - Test coordinated concurrent acquisitions, sequential window expiry, successful reset, transient errors, global pruning without touching old keys, capacity/eviction, active-key behavior, and bounded key size. Run limiter/auth tests with `-race` and retain HTTP throttling integration tests.
   - Update ADR 0009 to describe atomic reservation and bounded state rather than the superseded check/record model.

2. **New commit — `refactor: consolidate http adapter helpers`**
   - Move shared `activeHouseholdID` and date parsing out of entry-specific files, and relocate non-household-specific `RequireAuthAPI` to shared middleware.
   - Remove unreachable anonymous branches from ordinary protected web handlers now that `RequireAuth` owns them; retain session extraction needed for user/token-hash IDs and retain the invitation exception.
   - Remove any remaining redundant web permission prechecks, using the finance policy only for presentation and services for enforcement.
   - Keep health tests in `health_test.go` and constructors' nil-logger behavior consistent. Run unit, race, integration, and authorization tests.

3. **`49a5b60` — `test: add end-to-end mvp workflow smoke coverage`**
   - Extend the existing public HTTP smoke coverage where needed to exercise the corrected register → household creation/activation → entry → rate → snapshot report and invitation/revocation workflows.
   - Include regression assertions for success/failure notices, nested invitation mismatch, JSON decode error classes, and authorization boundaries without duplicating every focused unit test.
   - Keep fixtures deterministic and real PostgreSQL coverage intact.

4. **`e54e6e1` — `docs: document local operations and known limitations`**
   - Document required environment loading, fresh-database migration policy for this rewrite, token-hash sessions and cleanup frequency, limiter bounds/single-instance scope, integer ID representation, JSON 400/413/415 behavior, atomic activation, consistent options, report snapshots, and template development checks.
   - Update known limitations without presenting deferred deployment/CSRF/distributed-limiting work as completed.

5. **`649146c` — `fix: keep built binaries on embedded assets`**
   - Preserve the embedded-asset fix and synchronize its configuration/run documentation with required `DATABASE_URL` and validated `APP_ENV` behavior.

6. **Final documentation fold or new documentation commit**
   - Update this plan's milestone status and its `docs/plans/README.md` index entry as needed; the plan's original documentation commit remains before Milestone 0.
   - Record milestone completion only after the actual checks pass. Do not modify the original MVP plan.
   - Review all plan, decision, and operations links and remove temporary `pre-review/*` refs only after final acceptance.

**Exit:** the complete rewritten application passes unit, race-relevant, integration, authorization, template, smoke, build, and documentation checks; local operation and remaining limitations are accurate.

## 12. Test strategy

### Unit and race tests

- Configuration allow-list/defaults/required values and log-level parsing.
- JSON media types, exactly-one-object decoding, body limits, error envelopes, and response writing.
- Handler path/query translation and service-error mapping with small recording stubs, not business-rule mocks.
- Pagination overflow and registration/description text validation.
- Login limiter atomicity, concurrency, pruning, bounds, eviction, reset, and race behavior.
- Rendering buffers, page/fragment status behavior, template parsing, accessibility associations, and selected/submitted values.

### PostgreSQL integration and concurrency tests

- Fresh migrations, normal upgrades, late-version rejection, and checksum preflight without partial application.
- Session cleanup, token-hash identity, atomic household create/activate rollback, and safe retry.
- Entry/rate integer IDs, pagination boundaries, ownership/admin rules, and cross-household access.
- Invitation nested mismatch, acceptance/revocation serialization, and no membership when revocation wins.
- Consistent personal/effective options and committed update responses.
- One-snapshot reporting under coordinated concurrent writes.

### HTML, JSON, HTMX, and smoke tests

- Exercise both adapters against the same services and assert equivalent authorization and validation outcomes.
- Verify HTMX failure/success statuses, selectors, targets, swaps, redirects, notices, and non-JS fallbacks.
- Verify ordinary static assets and root/nested dotfile rejection.
- Build Tailwind after template conversions and inspect rendered full pages/fragments, not only template parsing.
- Finish with `mise run check:all`; record any check that cannot run rather than claiming success.

## 13. Finding-to-milestone traceability

| Finding area | Remediation locations |
| --- | --- |
| Configuration loading | M0 commits 2 and 5; M5 operations docs |
| Primary-key strategy | M1 commits 1–2 and 5–6; M2 commits 2–4; M3 commits 1–2 and 4 |
| Main logging consistency | M0 commit 2 |
| Web package | M0 commit 5; M1 commits 4 and 9; M2 commits 3–5; M3 commit 4; M4 commits 6–9; M5 commit 2 |
| JSON API package | M0 commit 2; M1 commits 6–9; M2 commit 4; M3 commit 4; M4 commits 2 and 5–6; M5 commits 1–3 |
| Remaining internal packages | M0 commit 3; M1 commits 1–2 and 6; M2 commit 3; M3 commit 3; M4 commit 2; M5 commit 1 |
| Template design system | M4 commits 7–9 and related rendering tests throughout |

## 14. Acceptance criteria

- Configuration is explicit and validated, and all logger fallbacks share application formatting/metadata.
- Migration preflight rejects changed history and backfilled versions before applying pending SQL.
- Primary keys match the confirmed table strategy, with no residual UUID session/membership/entry/rate IDs or stale fixtures.
- Static dotfiles are unreachable and ordinary assets remain available.
- Household creation and activation either both commit or both roll back in HTML and JSON.
- Expired sessions are cleaned up under the bounded-frequency policy; login throttling is atomic, bounded, race-tested, and retains generic errors.
- Registration names and descriptions reject PostgreSQL-invalid text before persistence; pagination offsets cannot overflow into database errors.
- API panics honor JSON/commitment rules, and JSON decoding returns consistent safe 400/413/415 responses.
- Entry/rate path IDs reject invalid integers and retain household authorization in services.
- Failed HTMX deletes never advertise success, and reports/preferences/rates use resource-appropriate error messages.
- Invitation revocation binds both path IDs and serializes correctly with acceptance.
- Personal/effective options and report summaries have their confirmed consistency semantics.
- Dead view state and duplicate helpers are removed; rendering, authentication, and authorization-policy ownership are clear.
- Semantic CSS and shared typed template components preserve existing behavior/appearance while standardizing form-error accessibility.
- Focused adapter tests complement, rather than replace, real PostgreSQL integration and end-to-end coverage.
- Every amended commit has a freshly reviewed message and relevant passing checks; every milestone and the final history pass their required validation.
- The original MVP plan remains unchanged, and the new plan/index and operational documentation reflect the delivered state.

## 15. Implementation status

| Milestone | Status |
| --- | --- |
| 0 — Project bootstrap | Pending remediation |
| 1 — Identity and household vertical slice | Pending remediation |
| 2 — Income and spending | Pending remediation |
| 3 — Exchange rates and summaries | Pending remediation |
| 4 — Invitations, preferences, and template reuse | Pending remediation |
| 5 — Local quality and handoff | Pending remediation |

No remediation checks are claimed by this planning document. Update this status in place as milestones are completed, and record unresolved implementation questions here rather than silently changing the confirmed decisions above.
