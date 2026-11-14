# Backend Review Remediation

Status: Proposed — not started. Nothing in this plan has been implemented. It records the findings of a full read of `backend/` so the work can be scheduled, amended, or rejected without re-deriving the analysis.

## 1. Purpose and context

A code review of `backend/` applied these filters: YAGNI, idiomatic Go, self-explanatory code, test quality, security, and edge cases. The review read every non-test Go file, all eight SQL migrations, the HTML templates and browser JavaScript, the CI workflow, and the local task definitions.

This plan does not redefine the product. It is a remediation plan for the reviewed implementation and sits alongside:

- [Budget Tracker MVP](./20261108_201136_budget_tracker_mvp_plan.md) — product baseline; do not edit.
- [Code Review Remediation](./20261113_163019_code_review_remediation_plan.md) — previous remediation; complete.
- [Test Assertion Library Adoption](./20261114_191415_test_assertion_library_adoption.md) — complete; its Testify conventions and narrow linter configuration are prerequisites already delivered. This plan owns the broader linter rollout and its findings, not another assertion migration.

### Rebase review after assertion adoption

Reviewed the fourteen assertion-adoption commits originally in `278f13297b73..97217f1` (pre-reorder commit IDs, in delivery order; this planning commit now follows them): `58c45ed` (dependency, conventions, tooling, config exemplar), `8d3d072` (logging), `d1c355f` (store unit), `a6ae9d8` (web), `da43f60` (auth unit), `580e9b4` (reporting unit), `ca91eca` (finance unit), `1ad4e72` (JSON adapters), `b824a34` (server integration), `c5b2ecb` (households integration), `5fd249b` (finance integration), `d5d23d7` (auth integration), `0e3bd37` (reporting integration), and `97217f1` (store integration). These change assertions, dependencies, tooling, and conventions; they do not implement this plan's production fixes or add its missing regression scenarios.

The completed assertion plan records 48 test files, 177 test functions, 29 subtest sites, and 1,167 Testify calls. Use the converted tests as the baseline, not the old assertion counts or source line numbers. In particular, `TestSummaryConcurrentWritesRemainConsistent` still checks only internal identities (T10), and ordinary protected web handlers still duplicate anonymous redirects (Y6). These follow-ups from the completed Code Review Remediation plan remain owned here; do not reopen or rewrite either completed plan.

Current tooling is Go `1.27.3`, `golangci-lint` `2.15.0`, and Testify `v1.12.1`. `mise run lint` already runs both `go vet` modes and both `golangci-lint` modes. `backend/.golangci.yml` enables only `testifylint`, with `require-error` disabled intentionally. CI already invokes `mise run check:all`, which includes `lint`; no new tool installation or CI lint step is needed.

During this documentation revision, `mise run lint` passed with zero issues in both configured linter modes. A read-only probe of the standard set plus configured `testifylint` found **14 plain-mode / 64 integration-mode findings**, not the earlier preparation estimate of 12. The exact inventory and rollout are in §4.6 and Milestone 0. The probe is intentionally failing; it does not mean the currently configured lint task fails. No application or integration tests were rerun for this documentation-only revision; the completed assertion plan retains its own validation record.

### Historical baseline at time of original review (before assertion adoption)

| Check | Result |
| --- | --- |
| `gofmt -l .` | clean |
| `go vet ./...` | clean |
| `go vet -tags=integration ./...` | clean |
| `go build ./...` | ok |
| `go test -count=1 ./...` | green |
| `TEST_DATABASE_URL=... go test -count=1 -tags=integration ./...` | green (Compose PostgreSQL 18 on `localhost:5433`) |

Integration coverage (per package, `-cover`): `auth` 87.9%, `config` 100%, `finance` 77.1%, `households` 80.7%, `httpapi` 61.4%, `logging` 100%, `reporting` 83.1%, `server` 97.7%, `store` 76.2%, `web` 20.0% (most `web` behavior is exercised from `internal/server` tests, so this number understates effective coverage).

Overall assessment: the architecture is sound — thin handlers, authorization in services, exact decimal/rational money math, snapshot-consistent reports, hashed session/invitation tokens, parameterized SQL, bounded JSON bodies, and real security headers. Findings below are hardening and consistency work, not redesign.

## 2. Goals

- Remove the realistic security weaknesses: login timing enumeration, unthrottled registration, bounded-but-evictable login limiter, and invitation tokens reaching logs.
- Make failure modes consistent: invalid input yields a validation error, missing rows yield `404`, and invalid UTF-8 never reaches PostgreSQL as a `500`.
- Make household scoping and date handling consistent across the HTML and JSON adapters.
- Make the schema enforce invariants the services already assume.
- Remove dead code and align naming with behavior.
- Close the specific test gaps that let the above issues exist, using the adopted Testify conventions.
- Extend the existing linter configuration to the explicit production linter set, fixing both production and test findings without weakening Testify enforcement.

## 3. Non-goals

- No new product features, UI redesign, or new endpoints.
- No change to money, exchange-rate precedence, attribution, or authorization rules.
- No distributed rate limiter, background workers, or multi-instance coordination (still a documented single-instance MVP).
- No `__Host-` cookie prefix, Origin/Referer validation, or signed CSRF cookie unless the single-host threat model changes.
- No framework, SPA, template engine, or generic abstractions.
- No arbitrary coverage targets; add the specific tests listed in §§4–5.
- No new assertion library, wholesale test restructuring, shared-test-double package, or subtest extraction. Those deferred assertion-plan observations are not required to deliver this remediation.

## 4. Findings inventory

Severity: **H** = fix before any deployment, **M** = correctness/hardening, **L** = polish. IDs are stable so commits and discussions can reference them.

### 4.1 Security

| ID | Sev | Finding | Location |
| --- | --- | --- | --- |
| S1 | H | Login is a user-enumeration timing oracle: a missing email returns before any Argon2 work, while a present email always pays the hash. Bodies and status match; timing does not. | `internal/auth/service.go:145-177` |
| S2 | H | Registration reveals that an email is already registered and is not throttled. Combined with unbounded Argon2 work per request (64 MiB) this enables enumeration and a work amplifier. | `internal/auth/service.go:87`, `internal/auth/validation.go:31-45`, `internal/web/auth_handlers.go:108-169` |
| S3 | H | Login has no maximum password length before Argon2; registration caps at 128 bytes. | `internal/web/auth_handlers.go:52-94` → `internal/auth/service.go:171` |
| S4 | H | The login limiter can be evicted on demand: keys are capped at 10,000 and the oldest most-recent-attempt key is evicted, so churning junk keys resets a victim key's budget. | `internal/auth/limiter.go:110-122`, key built at `internal/web/auth_handlers.go:214-221` |
| S5 | H | Raw invitation tokens reach logs. `RequestLogger` redacts `/invitations/`, but HTML error/template logging prints `r.URL.Path` verbatim. | `internal/web/render.go:161-163,187-189`, `internal/web/entry_handlers.go:470` |
| S6 | L | Double-submit CSRF relies on an unsigned, non-`__Host-` sibling cookie. Accepted under the current single-host model; document the assumption if deployment changes. | `internal/httpapi/csrf.go` |
| S7 | L | No explicit `http.MaxBytesReader` for HTML form posts; CSRF's `PostFormValue` can force multipart parsing before the handler. | `internal/httpapi/csrf.go:50-81`, `internal/server/server.go:124-136` |

### 4.2 Correctness and edge cases

| ID | Sev | Finding | Location |
| --- | --- | --- | --- |
| C1 | H | `validateName` does not check `utf8.ValidString`. Confirmed empirically: `validateName("B\xffd")` returns no error, and PostgreSQL then rejects the insert (`invalid byte sequence for encoding "UTF8"`), producing a `500`. | `internal/households/service.go:307-318` |
| C2 | M | Lost-update race: `UPDATE ... RETURNING` by ID after a load/authorize, so a concurrent delete yields `pgx.ErrNoRows` wrapped as a server error (`500`) instead of `ErrNotFound`. | `internal/finance/service.go:370-381`, `internal/finance/exchange_rates.go:190-209` |
| C3 | M | HTML edit/update reject entries from another household, but delete does not. Service authorization still holds; the adapters are inconsistent. | `internal/web/entry_handlers.go:431-455` vs `:296-342`, `:354-419` |
| C4 | M | "Today" is UTC in services but local time in HTML defaults. On a non-UTC host the preselected date can disagree with the service default. | `internal/finance/service.go:629-632`, `internal/web/entry_handlers.go:220`, `internal/web/report_handlers.go:42-50` |
| C5 | L | Query-parameter errors return `400`, equivalent service validation returns `422` (`page=abc` vs `page=-1`). | `internal/httpapi/entries.go:77-84,313-323`, `internal/finance/service.go:430-445` |
| C6 | L | `FormatAmount`/`DisplayAmount` silently return `"0"` for an unsupported `minorUnits`, hiding bad currency data. | `internal/finance/money.go:69-85,103-105` |
| C7 | L | Expired invitations are listed under "Pending invitations" with a past expiry date. | `internal/households/invitations.go:137-167`, `web/templates/household_settings.html:68-82` |
| C8 | M | The schema does not tie `incomes`/`spendings.user_id` to household membership, and `users` names have no DB length constraints although the app enforces them. | `backend/db/migrations/0005_incomes_spendings.sql`, `0002_identity.sql` |
| C9 | L | Residual inconsistencies: malformed household ID renders a plain-text 404 instead of the styled error page; invite acceptance continues after `SetActiveHousehold` fails; report conversion overflow aborts the whole report; `canonicalRate` accepts scientific notation and rejects exactly-representable values with >8 trailing-zero decimals. | `internal/web/household_handlers.go:356-364`, `internal/web/invitation_handlers.go:88-90`, `internal/reporting/service.go:169-172`, `internal/finance/exchange_rates.go:306-322` |

### 4.3 YAGNI / dead code

| ID | Finding | Location |
| --- | --- | --- |
| Y1 | `invitationByID` is never called. | `internal/households/invitations.go:362-377` |
| Y2 | `GetUserOptions` and `EffectiveOptions` wrap `GetOptionsView` and are used only by tests. | `internal/households/options.go:111-127` |
| Y3 | The negative branch of `roundHalfEven` is unreachable: `ConvertMinor` rejects negative amounts and non-positive rates. | `internal/reporting/rates.go:136-144` |
| Y4 | `decodeJSON` takes an unused `http.ResponseWriter`. | `internal/httpapi/json.go:89` |
| Y5 | `queryRower` only declares `Exec`; the name promises `QueryRow`. | `internal/auth/service.go:303-305` |
| Y6 | Nearly every handler repeats session-presence checks already guaranteed by `web.RequireAuth` (the sole real exception is `AcceptInvitation`). | `internal/web/household_handlers.go:47-51,105-109,140-144,177-181,215-219,277-281`; `internal/web/entry_handlers.go:208-212,238-242,297-301,355-359,432-436` |
| Y7 | Three near-identical validation-error types and three date parsers with slightly different trimming behavior. | `internal/auth/validation.go`, `internal/finance/service.go:74-92`, `internal/households/options.go:230-259`, `internal/httpapi/shared.go:32-41`, `internal/web/helpers.go:8-18`, `internal/web/entry_handlers.go:618-624` |
| Y8 | `ExchangeRate.RateString` hand-trims numeric text instead of using the existing `shopspring/decimal` dependency. | `internal/finance/exchange_rates.go:35-45` |

### 4.4 Idiomatic Go and clarity

| ID | Finding | Location |
| --- | --- | --- |
| I1 | `Kind.table()` silently returns `spendings` for any non-income value; callers validate first but the assumption is implicit. | `internal/finance/service.go:47-52` |
| I2 | `auth.Service` logs through the package-level `slog.Warn` instead of an injected logger (works only because `main` sets the default). | `internal/auth/service.go:254` |
| I3 | Expiry uses `time.Now()` directly while a `now` seam exists for cleanup, so expiry tests cannot be clock-driven. | `internal/auth/service.go:73-83,101,312` |
| I4 | `finance` uses `errors.New` for user-facing validation sentences; six punctuation-bearing returns trigger `staticcheck` ST1005. Preserve the user-facing copy while separating it from ordinary Go errors (L2). | `internal/finance/service.go:608-627`, `internal/finance/exchange_rates.go:306-322` |

### 4.5 Test gaps

| ID | Gap | Related finding |
| --- | --- | --- |
| T1 | No non-integration test file for `internal/households`; `validateName` has no invalid-UTF-8 case. | C1 |
| T2 | No cases for `minorUnits` = 4 or invalid values (5, −1) in money format/parse. | C6 |
| T3 | No test for a row disappearing between load and write. | C2 |
| T4 | No test for active-household scoping on delete. | C3 |
| T5 | No test for `ConvertMinor` overflow. | — |
| T6 | Limiter tests cover capacity/eviction but not the bypass effect. | S4 |
| T7 | No end-to-end test asserting invitation tokens are absent from logs. | S5 |
| T8 | `safeNextPath` lacks `/\evil.example`, `/%2f%2fevil`, and query-only-change cases. | — |
| T9 | Partially addressed by assertion adoption: `mise run lint` now includes `testifylint` in both build-tag modes, but does not yet enable production analysis such as `unused` for Y1. Extend the existing tool/config rather than adding a second linter. | §4.6; Milestone 0 |
| T10 | No test proves report summaries are read from one database snapshot. `TestSummaryConcurrentWritesRemainConsistent` (`internal/reporting/service_integration_test.go`) has its writer commit options, rates, and entries as separate statements and asserts only internal identities (included + missing = total, net = income − spending, currency minor units), which also hold under `READ COMMITTED`. This leaves the snapshot requirement from the historical remediation plan (M3) unverified. | — (historical plan M3) |

### 4.6 Broader linter findings (new rollout scope)

Inventory at the assertion-adoption tip (originally `97217f1`, before moving this planning commit after it), using the existing configuration with `--default=standard` and issue caps disabled. The standard linters are `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`; configured `testifylint` remains enabled too.

| ID | Finding | Plain | Integration | Location / treatment |
| --- | --- | --- | --- | --- |
| L1 | Unchecked errors (`errcheck`) | 7 | 57 | Three `fmt.Fprint*` calls in `cmd/migrate/main.go:94,108,113`; four response-body closes in `internal/web/web_test.go:234,251,299,324`; fifty more closes in integration-tagged `internal/server` tests. Propagate migration output errors; explicitly handle test cleanup errors. |
| L2 | User-facing sentences represented as ordinary errors (`staticcheck` ST1005) | 6 | 6 | Four returns in `canonicalRate` and two in `normalizeDescription`. Keep field-validation copy intact using a small, explicit representation owned by `finance`, rather than suppressing ST1005 or globally lowercasing UI messages (I4). |
| L3 | Unused `invitationByID` (`unused`) | 1 | 1 | Same work as Y1; remove once, not as a second cleanup. |
| **Total** | | **14** | **64** | The tagged total includes the plain findings; do not add the totals together. |

Reproduce from the repository root without changing tracked configuration:

```sh
mise exec -- sh -c 'cd backend && golangci-lint run --default=standard --max-same-issues=0 --max-issues-per-linter=0 ./...'
mise exec -- sh -c 'cd backend && golangci-lint run --default=standard --max-same-issues=0 --max-issues-per-linter=0 --build-tags=integration ./...'
```

Without these output flags the tool reports only 10 findings per mode because repeated issues are capped; that is not a complete inventory. This also corrects the completed assertion plan's preparation estimate in this active follow-up, leaving its historical record unchanged. L1 includes test code: enabling the broader set is not a production-only change.

## 5. Proposed milestones

Each milestone is one or more small commits that leave the tree buildable, formatted, vetted, linted in both build-tag modes, and tested. Follow the layer order from `AGENTS.md`: migration → model/query → service → API → UI. Add or update the tests for a behavior in the same commit as that behavior.

All new or modified tests follow `AGENTS.md`: expected before actual; `require` for setup, decoding, gating statuses, and values subsequently indexed/dereferenced/type-asserted; `assert` for independent checks and independent expected errors. Concrete error-pointer results need `Nil`/`NotNil`, not `NoError`/`Error` (typed-nil interfaces). Never use fatal assertions in worker goroutines, including through helpers; report nonfatally and return, or assert collected results in the test goroutine. Review this manually because `testifylint` does not enforce the worker rule. Keep response-consuming diagnostics such as `readBody` on failure paths only; Testify message arguments are evaluated even on success. Retain protocol/fixture helpers, but do not introduce single-comparison wrappers.

### Milestone 0 — Extend the existing lint baseline (L1–L3, T9, Y1, I4)

Use three small, independently green commits; each enables its linter(s) only together with the fixes required in **both** build-tag modes:

1. **Handle unchecked errors and enable `errcheck`.** Return migration status-output write errors and retain the final flush error. Add a focused failing-writer regression test with a small output helper if needed. Handle the four web and fifty tagged server response-body closes explicitly, preserving response consumption, close timing, and failure diagnostics. Use nonfatal assertions for independent cleanup errors; do not defer a fatal assertion or move a close into an eagerly evaluated message. Do not blanket-ignore close errors or add a global exclusion. Enable `govet` and `ineffassign` alongside `errcheck` (the probe found no additional issues for these).
2. **Separate validation messages and enable `staticcheck`.** Resolve all six L2 returns without changing rendered field messages, `422` mapping, or validation fields. Add focused tests for `canonicalRate`/`normalizeDescription` and adapter copy as appropriate. A small finance-local validation-message type is sufficient; no generic errors framework. This settles I4 instead of merely documenting the current punctuation and leaving lint red. Do not change rate-notation policy in this commit; Q4 remains for Milestone 3.
3. **Remove `invitationByID` and enable `unused`.** This delivers Y1/L3. Do not remove Y2's exported wrappers solely because `unused` does not flag them; review them separately in Milestone 6. Update `AGENTS.md` and `docs/operations.md` to describe the expanded set and existing two-mode commands.

Final configuration keeps `default: none` and explicitly enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, `testifylint`, and `unused`, freezing the current standard set rather than adopting future defaults implicitly. Keep `testifylint` settings, including disabled `require-error`, unchanged. No lint exclusions, `nolint` blanket suppressions, or weakened checks to achieve green. Keep the existing tool pin, four-run `lint` task, and CI wiring; do not add standalone `staticcheck` installation/runs. Any additional findings discovered at implementation time are recorded here and fixed with the enabling commit before proceeding.

Acceptance: `mise run lint` reports zero issues in both modes at every commit, with all six linters enabled by the end; relevant unit/integration tests pass; T9 and L1–L3 are closed. Review the widened output with issue caps disabled as well, and record exact checks when delivered.

### Milestone 1 — Authentication hardening (S1–S4)

Commits:

1. **Equalize login failure timing.** Add a package-level dummy Argon2id hash (generated at init from a random password, or a fixed encoded hash with the same parameters) and verify against it when the email lookup misses. Keep `ErrInvalidCredentials` and keep registration hashing before the uniqueness check. Tests: unit test that a missing email and a wrong password take the same path (assert `VerifyPassword` is invoked via a small seam, not wall-clock timing).
2. **Bound login password input.** Reject passwords longer than the registration maximum (128 bytes) with `ErrInvalidCredentials` before hashing. Tests: table case alongside the existing validation tests; handler test that an oversized password cannot reach `Login`.
3. **Throttle registration.** Reuse/extend the limiter with a registration policy (proposed: 5 attempts per 15 minutes per IP + normalized email, mirroring login). Decide whether the email-taken message stays (see Q2). Tests: limiter unit test, HTML handler test that the throttle renders a `429`.
4. **Make limiter eviction non-bypassable.** Proposed approach: keep the existing per-email key and add a per-IP admission budget so creating 10,000 keys requires 10,000 IPs; alternatively refuse new keys at the cap and let maintenance reclaim them. Do not silently evict live counters. Tests: extend `limiter_test.go` with the churn scenario.

Acceptance: login timing is uniform by construction; registration is throttled; the churn scenario no longer resets a key; all existing auth/server tests pass unchanged.

### Milestone 2 — Logging and HTTP hygiene (S5, S7, Y4, C5, C9 partial)

Commits:

1. **Redact invitation tokens in all logs.** Add a small `web` helper (for example `logPath(r)`) that maps `/invitations/*` to `/invitations/[redacted]`, and use it in `internalError`, `renderTemplate`, and `renderEntryDeleteError`. Test: drive a failing invitation handler and assert the captured logger output contains no token (T7).
2. **Remove the unused `decodeJSON` parameter** and update all call sites. Mechanical; existing `json_test.go` must pass.
3. **Align query error statuses.** Reject negative `page`/`page_size` in `httpapi.parseOptionalInt` with `400 invalid_request`, leaving service validation as defence in depth (C5). Test: `page=-1` and `page_size=-1` return `400`; zero still means "use default".
4. **Optional: explicit form body limit.** Add a `http.MaxBytesReader`-based middleware (or set it at the start of form handlers) so HTML POSTs have the same explicit bound as JSON. Only do this if it stays a small, well-tested middleware.
5. **Use the styled error page for malformed household IDs** (C9) by returning an error to callers, as the other path helpers already do.

Acceptance: no path logged by the HTML adapter can contain an invitation token; API query errors are consistently `400`; malformed path IDs render the normal error page.

### Milestone 3 — Domain correctness (C1, C2, C3, C7, C9)

Commits:

1. **Validate UTF-8 household names.** Add `utf8.ValidString` to `validateName`, mirroring `validatePersonName` and `normalizeDescription`. Add `internal/households/service_test.go` (non-integration) covering invalid UTF-8, control characters, whitespace-only, and the 100-rune boundary (T1).
2. **Map missing rows during writes to `ErrNotFound`.** In `finance.Update` and `UpdateExchangeRate`, check the `Scan` error for `pgx.ErrNoRows` and return `ErrNotFound`. Add integration tests that simulate the race by deleting the row between load and write (use a service seam or delete inside a transaction the service can observe) (T3).
3. **Scope HTML delete to the active household.** Add the same `entry.HouseholdID != data.ActiveHousehold.ID` guard used by edit/update, or remove the guard from all three and document that the service owns scoping. Prefer the former to preserve current behavior. Add a handler test (T4).
4. **Label expired invitations.** Add an `Expired(now)` method or an `Available` field on `Invitation`, filter or label expired rows in `ListInvitations`/the settings template, and test the boundary at exactly `ExpiresAt`.
5. **Address the remaining C9 items** in separate small commits where the concerns differ: decide whether to surface the `SetActiveHousehold` failure after acceptance, and decide whether a conversion overflow should abort the report or mark it incomplete (Q3). The styled error page is already in Milestone 2. Resolve exponent notation per Q4 with table-driven `canonicalRate` tests; the current parser already rejects an exponent below −8, including extra trailing-zero decimals, so do not schedule that existing rejection as a new fix. Explicitly test the intended scale boundary and preserve Milestone 0's validation-message representation.

Acceptance: invalid UTF-8 yields `422` in both adapters; a vanished row yields `404`; delete matches edit/update scoping; expired invitations are visibly expired.

### Milestone 4 — Schema integrity (C8)

New migration `0009_entry_membership_and_name_limits.sql` (backward compatible, in its own commit before any code that relies on it):

1. Add `incomes`/`spendings` composite FK `(household_id, user_id) → household_memberships(household_id, user_id)`. NULL `user_id` (household-wide entries) is exempt by SQL semantics. Add it as `NOT VALID` and then `VALIDATE CONSTRAINT`, so an existing database can be checked without a long exclusive lock. Choose the delete behavior explicitly (Q5); the proposal is `ON DELETE CASCADE` to match the current `users` cascade, documented in the migration comment.
2. Add `users_first_name_length` / `users_last_name_length` checks matching the 80-rune application limit.

Tests: integration test that inserting an entry for a non-member fails; test that the existing service paths still pass; `mise run db:reset` on a disposable database then full integration run.

Acceptance: migration applies and validates; failure rolls back the migration transaction cleanly on an invalid fixture; no service behavior changes. The migration runner is forward-only, so this does not require a new down-migration mechanism.

### Milestone 5 — One date clock (C4)

Commits:

1. Use UTC for every user-visible default date: `web.newEntry` (`time.Now().UTC()`), report `from`/`to` defaults, and rate `rate_date` defaults. Keep `finance.today()`/`dateOnly` as the single source. Tests: assert the new-entry form's default date equals `finance.Today()`-style UTC date; add a boundary test around 00:00 UTC.

Acceptance: HTML defaults and service defaults never disagree, regardless of host timezone.

### Milestone 6 — Remaining dead code and clarity (Y2–Y8, I1–I3)

No behavior change; existing tests are the safety net. One commit per concern:

1. Y1 will have been delivered by Milestone 0 before this milestone; do not repeat it. Delete or justify `GetUserOptions`/`EffectiveOptions`; if deleted, move the converted Testify assertions in `options_integration_test.go` onto `GetOptionsView` (Y2).
2. Delete the unreachable negative branch of `roundHalfEven` (Y3).
3. Remove the unused `decodeJSON` parameter (Y4; already done in Milestone 2 if preferred).
4. Rename `queryRower` to reflect `Exec` (Y5).
5. Remove redundant anonymous-session checks and `/login` redirects from ordinary handlers whose routes are protected by `RequireAuth` (Y6). Retain session extraction needed for user/token-hash IDs and preserve `AcceptInvitation` as the invitation-aware exception. Do not merely move the redundant redirects into a shared helper. Add wrapped-route tests proving unauthenticated requests redirect without invoking services, authenticated requests still reach the handlers, and invitation acceptance retains its safe return path. This closes the authentication-deduplication gap from the historical remediation plan (M5).
6. Consolidate the validation-error and date-parsing helpers where the consolidation is mechanical (Y7). Do not introduce a `utils` package; put shared helpers in the package that owns the behavior.
7. Reimplement `RateString` with `decimal` (Y8).
8. Make `Kind.table()` return `("", error)` or document the internal validation invariant (I1). Pass a logger into `auth.Service` or document the `slog.Default` dependency (I2). Inject the clock for expiry if the test seam lands cleanly (I3). I4 will have been resolved in Milestone 0; retain its distinction between user-facing field messages and ordinary Go errors.

Acceptance: `mise run lint` (including the expanded linter set in both modes) and tests pass; no exported API removed outside the module; cleanup diff is net-negative in lines. Ordinary protected handlers no longer duplicate anonymous redirects; wrapped-route tests verify authentication enforcement and the invitation-aware exception.

### Milestone 7 — Remaining test coverage and final validation (T2, T5, T8, T10)

1. Add the missing table cases: money `minorUnits` 4 and invalid units (T2), `ConvertMinor` overflow (T5), `safeNextPath` hostile paths (T8).
2. Add an integration assertion that an oversized rate or amount is rejected without a partial write.
3. Add the coordinated report-snapshot regression test (T10): seed known states, commit one atomic change between controlled report reads, and assert an outcome that is possible only under a single `REPEATABLE READ` snapshot rather than arithmetic identities computable from the returned summary. Verify the test fails with the service temporarily switched to `READ COMMITTED` using a throwaway local edit to the `pgx.TxOptions` in `internal/reporting/service.go`; do not commit a test-only isolation seam for it.
4. Confirm T6/T7 landed with Milestones 1/2 and T9 with Milestone 0; no final test-only catch-up for behavior shipped earlier. Run the security/concurrency tests affected by this plan with `-race`, including the limiter and coordinated report test, and record the exact commands and results.
5. Re-run `mise run check:all` and record the result in this plan. Confirm CI continues to enforce the expanded configuration through the existing `lint` task; no additional workflow step is needed.

Acceptance: every finding in §4.5 is either covered by a test or explicitly waived with a reason; CI enforces both build-tag lint modes and the existing check suite.

## 6. Acceptance criteria for the whole plan

- `mise run format:check`, `mise run lint`, `mise run test`, `mise run test:integration`, and `mise run build` pass at every implementation commit; CI (`.github/workflows/ci.yml`) runs the same set via `check:all`. Documentation-only commits require content/link review, not application tests.
- The explicit six-linter configuration from Milestone 0 is active in plain and integration-tagged modes with zero findings. Testify conventions and the intentional `require-error` setting remain intact; no broader rule is suppressed to conceal findings.
- No invitation token, session token, or password appears in any log line on any error path.
- No user-controlled invalid UTF-8 can reach PostgreSQL; such input yields a field-level validation error.
- HTML and JSON adapters agree on status codes for the same input class, and both rely on service authorization rather than handler prechecks for resource access.
- Existing product rules are unchanged: members manage their own entries; admins view all, manage settings/invitations, and modify household-wide entries; all members may manage exchange rates; reports never silently omit entries.

## 7. Sequencing and commit discipline

Recommended order: Milestone 0 → 1 → 2 → 3 → 5 → 4 → 6 → 7. Assertion adoption has already landed and is not a pending sequencing choice. The migration in Milestone 4 is independent and backward compatible; land it whenever the code change that relies on it is ready, but before that change. Milestones 1–3 are security/correctness fixes and should not be bundled with the cleanup in Milestone 6.

- One concern per commit; subject in Conventional Commit form per `.agents/skills/commit-message/SKILL.md`.
- Tests for a behavior land in the same commit as the behavior.
- Migration commits come before code that depends on the new constraint.
- Do not redo the completed assertion-library migration. New tests use Testify immediately, and lint-enabling commits include their own production and test fixes. Do not widen the config in one commit and leave its findings for a later one.
- Completed plans remain historical records; this proposed plan owns the corrected lint inventory and the outstanding Y6/T10 acceptance gaps.

## 8. Unresolved questions (confirm before starting)

- **Q1 — Registration throttling policy.** Is "5 per 15 minutes per IP + email" acceptable, or should registration get a stricter per-IP budget and a CAPTCHA-style gate later?
- **Q2 — Email enumeration at registration.** Keep the explicit "account already exists" message (better UX, enumerable) or switch to a generic response? The proposal keeps the message and relies on throttling; the alternative is a generic 422 plus an out-of-band email.
- **Q3 — Report overflow behavior.** Abort with an error (current) or mark the summary incomplete with a missing/overflow reason? The proposal keeps the explicit error and documents it.
- **Q4 — Rate input notation.** Reject scientific notation outright, or accept it and canonicalize? The proposal rejects it to match the form copy ("at most 8 decimal places").
- **Q5 — Membership FK delete behavior.** `ON DELETE CASCADE` (matches current `users` cascade) or `RESTRICT` (forces an explicit future member-removal workflow)? The proposal uses `CASCADE` and documents that no member-removal feature exists yet.
- **Q6 — CSRF hardening.** Confirm that the single-host, no-sibling-subdomain assumption still holds; if not, promote S6 from "document" to "fix" (signed cookie or Origin validation).
- **Q7 — Scope of Y7 cleanup.** Is mechanical consolidation of validation-error and date-parsing helpers in scope now, or should it wait for the next feature that touches those files? Y6 is settled: remove redundant anonymous checks from protected handlers as specified in Milestone 6.

## 9. Finding → milestone map

| Finding | Milestone |
| --- | --- |
| S1, S2, S3, S4 | 1 |
| S5, S7, Y4, C5, C9 (plain 404) | 2 |
| C1, C2, C3, C7, C9 (rest) | 3 |
| C8 | 4 |
| C4 | 5 |
| L1–L3, T9, Y1, I4 | 0 |
| Y2, Y3, Y5, Y6, Y7, Y8, I1–I3 | 6 |
| T1, T3, T4 | 3 |
| T2, T5, T8, T10 | 7 |
| T6 | 1 |
| T7 | 2 |
| S6 | Documentation only; revisit if Q6 changes |
