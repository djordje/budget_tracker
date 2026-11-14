# Test Assertion Library Adoption

Status: Complete — delivered and validated. Section 11 records the inventory and checks; sections 1–10 are the plan as executed.

## 1. Purpose and context

The test suite uses only the standard `testing` package. Every assertion is a hand-written conditional with `t.Errorf`/`t.Fatalf`. This plan evaluates replacing that with the assertion library the Go community most commonly uses, and records everything needed to execute the migration later without re-deriving the analysis.

This plan does not change product behavior, HTTP contracts, database schema, or application architecture. It is a test-code-only initiative. Related plans: [Budget Tracker MVP](./20261108_201136_budget_tracker_mvp_plan.md), [Code Review Remediation](./20261113_163019_code_review_remediation_plan.md) (historical, complete, left unedited), and [Backend Review Remediation](./20261114_192841_backend_review_remediation.md) (proposed follow-up; carries the remaining acceptance gaps). This migration must preserve the tests these plans require.

Land order is settled: this plan goes first, because it is mechanical and behavior-free. It establishes the testifylint rule set and the `mise.toml` tool pin. The Backend Review Remediation plan is then reworked on top of the converted suite: it writes its new test cases in the adopted style and raises the linter set to include the production linters, fixing the findings that enabling them surfaces. That inverts the sequencing note currently recorded in that plan; update it when that plan is reworked, not before.

### Baseline at time of writing

| Check | Result |
| --- | --- |
| `go vet ./...` | clean |
| `go test ./...` | green |
| Integration tests | behind `//go:build integration`, require Compose PostgreSQL, not run for this analysis |
| Test-only dependencies in `go.mod` | none |

The baseline must stay green at every commit of this migration. There is no window where a package's tests are half-converted and unbuildable.

### Current suite size

48 `_test.go` files, 8,728 lines, **970 assertion sites**: 481 `t.Errorf`, 391 `t.Fatalf`, 58 `t.Error`, 40 `t.Fatal`.

| Package | Files | Lines | Assertion sites |
| --- | --- | --- | --- |
| `internal/server` | 10 | 2,467 | 275 |
| `internal/httpapi` | 10 | 1,584 | 138 |
| `internal/households` | 3 | 852 | 129 |
| `internal/finance` | 3 | 937 | 114 |
| `internal/auth` | 9 | 848 | 112 |
| `internal/web` | 6 | 905 | 65 |
| `internal/reporting` | 2 | 548 | 58 |
| `internal/store` | 3 | 425 | 56 |
| `internal/config` | 1 | 95 | 15 |
| `internal/logging` | 1 | 67 | 8 |

Other counts: 29 `t.Run` subtest sites, 42 non-`Test` helper functions, 183 `if err != nil { t.Fatal... }` blocks, **0** `t.Parallel()` calls.

## 2. Findings

These are the observations that motivate the plan. They were collected by reading and grepping the suite; they are not opinions about individual authors.

- **F1 — Assertions restate plumbing instead of intent.** The dominant shape is `if got != want { t.Errorf("thing = %v", got) }`, often across four lines. In `internal/httpapi/households_test.go` the authentication test spends 12 lines on what is conceptually two checks.
- **F2 — Failure messages frequently omit the expectation.** ~377 `t.Errorf`/`t.Fatalf` messages contain no `want`, of which ~139 print a produced value with no expected value to compare against (e.g. `t.Errorf("Location = %q", got)`, `t.Fatalf("status = %d", recorder.Code)`). A failure therefore reports the symptom but not the contract, which is the main thing a test failure has to communicate.
- **F3 — 98 bare `t.Error("...")`/`t.Fatal("...")` calls** carry no captured values at all, so a red test in CI says only that a sentence was false.
- **F4 — `Fatalf` vs `Errorf` is chosen ad hoc.** 391 `Fatalf` against 481 `Errorf`, with no stated rule. The consequence is inconsistent: some tests keep asserting after a fatal prerequisite has already failed (risking nil dereference noise), others abort before reporting unrelated independent failures.
- **F5 — No subtest/`t.Run` structure in 31 of 48 files**, 28 of which contain two or more test functions or a multi-phase scenario, including most integration tests. A failed case in a long scenario names the enclosing function, not the case.
- **F6 — Duplicated test doubles.** `stubSessionLookup` is defined three times (`internal/auth/middleware_test.go`, `internal/httpapi/households_test.go`, `internal/web/household_handlers_test.go`) with three different levels of fidelity. The household service double exists twice with different shapes: `stubHouseholds` (17 methods, `internal/web/web_test.go`) and `stubHouseholdService` (embedding + 9 methods, `internal/httpapi/households_test.go`). `internal/web/web_test.go` nil-checks its hook function; `internal/httpapi/households_test.go` does not, so an unexpected call panics in one package and silently succeeds with zero values in the other.
- **F7 — Repeated multi-line decode/assert sequences.** JSON decode then two field checks, or status check then body check, is copy-pasted across `httpapi` and `server` tests.

## 3. Goals

- Adopt the community-standard assertion approach with the smallest credible dependency set.
- Convert assertion sites mechanically and reviewably, package by package, keeping `go vet`, both `golangci-lint` modes, and `go test` green at every commit.
- Make value comparisons report expected and actual values; make error and invariant assertions report the relevant failure and context.
- Establish one rule for fatal vs non-fatal assertions and apply it uniformly.
- Enforce the adopted style mechanically from the first commit, so a later test cannot quietly reintroduce the old shapes.
- Leave the suite's structure (table-driven domain tests, real-PostgreSQL integration tests, adapter-boundary unit tests) unchanged, because those choices follow `AGENTS.md` and are not in question.

## 4. Non-goals

- No change to what the tests cover, except where F2/F3 messages are tightened while converting.
- No new integration harness, no `t.Parallel()` rollout, no coverage targets, no golden-file or snapshot testing.
- No replacement of application interfaces to make them easier to mock, and no interface-width refactor.
- No conversion of the `internal/testdb` harness or the `//go:build integration` mechanism.
- No test-framework rewrite (`ginkgo`/`gomega` suites, `testify/suite`). Suites are explicitly out of scope; keep plain `func TestXxx(t *testing.T)`.
- No subtest extraction or shared-test-double restructuring during this migration. F5 and F6 are observations for separately scoped follow-up work, not adoption acceptance criteria.
- No bespoke assertion wrapper package. Wrapping testify in project-specific helpers is the one thing that reliably makes a Go test suite feel non-standard.
- No broader linter set. This plan enables `testifylint` only; the default `golangci-lint` linters report 12 production-code findings and belong to the follow-up plan, together with the fixes they require. `go vet` remains the only non-testify static analysis, exactly as today.

## 5. Decisions (settled at review)

1. **Adopt `github.com/stretchr/testify` at `v1.12.1`** (current release) for assertions, using `require` and `assert` only. Resolved at review: a test-only module pulls exactly one indirect requirement (`go.yaml.in/yaml/v3`), so the dependency footprint is far smaller than a reviewer of the library policy would assume. Rationale: it is the most widely used test library in the Go ecosystem and the choice most likely to read as conventional to a future contributor or reviewer. This satisfies `AGENTS.md`'s "add libraries only when they solve a concrete problem": the concrete problem is repetitive assertion plumbing and inconsistent failure diagnostics across 970 hand-written assertion sites, some of which omit expectations.
2. **Start with Testify alone; do not add `github.com/google/go-cmp`.** Testify's `assert.Equal` already produces diffs for supported structs, slices, and maps. Reconsider `go-cmp` only when a concrete comparison needs its comparison options or semantics.
3. **Do not adopt `testify/mock`.** The existing func-field stubs are compile-time checked; `mock.On("Method", ...)` is stringly typed and would regress that.
4. **Assertion ordering follows testify**: `require.Equal(t, want, got)`. Do not introduce local helpers that flip the argument order; argument-order mistakes obscure failure diagnostics. `testifylint`'s `expected-actual` check catches literal-versus-runtime reversals, but it does not catch variable-to-variable swaps, so review must still verify operand ordering.
5. **Fatal/non-fatal rule**: `require` for prerequisites whose failure makes later assertions meaningless (successful setup, JSON decode, gating status code). `assert` for independent checks, including expected errors when no subsequent check depends on them. Never use `require`, `t.Fatal`, or `t.FailNow` from worker goroutines: retain nonfatal reporting with an explicit worker return, or collect results and assert from the test goroutine. Preserve synchronization and cleanup behavior. This replaces the current ad-hoc split (F4).
6. **Keep hand-written helpers where they encode a protocol**, not a single comparison. `sessionRequest`, `newTestClient`, `createAPIHousehold` and the fixture builders stay. But single-comparison helpers such as a local `wantStatus` must not be introduced, because testify replaces them.
7. **Prefer specialized matchers where they exist**: `assert.JSONEq` for JSON bodies, `ErrorIs` for sentinel errors (`auth.ErrSessionNotFound`) with fatal/non-fatal chosen by decision 5, `assert.Empty`/`assert.NotEmpty`, `require.Eventually` only if a concurrency test needs it.
8. **Keep integration tests in scope.** The 275 sites in `internal/server` and the other `integration`-tagged files are converted too, otherwise the suite ends up with two styles.
9. **Adopt `testifylint` now, narrowly; leave the broader linter set to the follow-up.** Resolved at review: enforcement ships with this migration rather than after it, but only in a configuration that is green at every commit. That means `golangci-lint` pinned in `mise.toml`, a repository `.golangci.yml` with `default: none` and `testifylint` as the only enabled linter, and two runs (`plain`, `--build-tags=integration`) so the 652 integration-tagged sites are covered. Verified before adoption: the current suite reports `0 issues` in both modes (testifylint has nothing to flag until testify imports exist), each run costs under a second, `mise run check`/`check:all` already invoke `lint` so CI enforces it with no workflow edit, and `expected-actual` fires on a converted-argument reversal in a real module copy. **`testifylint`'s `require-error` checker must stay disabled**, because it demands `require` for every error assertion and decision 5 requires nonfatal `assert.Error` for independent expected-error checks. The checker does not detect `require` inside a worker goroutine (verified with `go-require` force-enabled), so decision 5's worker rule remains a review duty. Widening the linter set is the follow-up plan's job, in the commits that fix those production findings.

## 6. Migration rules

Mapping applied mechanically during conversion. These are defaults, not an
unconditional rewrite: apply the worker-goroutine exception and the
expected-error rule from decision 5 before converting a site.

| Current | Replacement |
| --- | --- |
| `if err != nil { t.Fatalf("X() error = %v", err) }` | `require.NoError(t, err)`; inside a worker goroutine keep nonfatal reporting with an explicit return |
| `if err == nil { t.Fatal("expected error") }` | `require.Error(t, err)` when the next check depends on the outcome, otherwise `assert.Error(t, err)` |
| `if !errors.Is(err, want) { t.Fatalf(...) }` | `require.ErrorIs`/`assert.ErrorIs(t, err, want)` per decision 5 |
| `if got != want { t.Errorf("x = %v, want %v", got, want) }` (prerequisite) | `require.Equal(t, want, got)` |
| same, but independent second check | `assert.Equal(t, want, got)` |
| recorder code gate | `require.Equal(t, http.StatusUnauthorized, recorder.Code, "body = %s", recorder.Body.String())` — keep the body as a failure message argument |
| decode + compare | `require.NoError(t, json.Unmarshal(...))`, then `assert.Equal`/`assert.JSONEq` |
| `if len(x) != want { t.Fatalf(...) }` | `require.Len(t, x, want)` whenever `x` is indexed or dereferenced afterwards |
| same, with nothing downstream touching `x` | `assert.Len(t, x, want)` |
| compound predicate (two or three conditions joined by `&&`/`or` in one `if`) with a single `%+v` message | one assertion per condition (`assert.Equal`, `assert.NotEmpty`, ...), repeating the original message argument on each |
| `if !strings.Contains(s, want) { ... }` | `assert.Contains(t, s, want)` |
| `if strings.HasPrefix(s, want) { ... }` / `if !ok { ... }` | `assert.True(t, strings.HasPrefix(s, want), "<context>")` / `assert.True(t, ok, "<context>")` |
| `if !limiter.TryAcquire(...) { ... }` | `assert.False(t, limiter.TryAcquire(...), "<context>")` |
| `if x == "" { ... }` / `if n == 0 { ... }` | `assert.Empty(t, x)` / `assert.Zero(t, n)` — never `assert.False(t, x == "")`, which `testifylint` rejects as `compares` |
| `if err != nil` whose message named the call | `require.NoError(t, err, "GetOptionsView()")` — keep the call-site label |
| `if x == "" { t.Error(...) }` | `assert.NotEmpty(t, x)` |
| bare `t.Error("invariant broken")` | keep as `assert.Fail(t, "invariant broken")` only if no value can be captured; otherwise capture values |

Fatal-ness is decided by downstream use, not by the old verb. If the checked value is indexed, dereferenced, or type-asserted after the check — including reading a field of a returned error, as `internal/auth/validation_test.go` does with `err.Fields[tt.field]` — the conversion is `require`, never `assert`. Converting such a gate to `assert` keeps the check but turns a clean abort into an index panic or nil dereference, and the plan's style rules forbid that control-flow change. Canonical cases: `internal/auth/cookies_test.go` (`t.Fatalf` then `cookies[0]`), `internal/httpapi/csrf_test.go` (same shape), `internal/auth/validation_test.go` (`if err == nil { t.Fatal }` then `err.Fields[...]`).

Conversion example, `internal/httpapi/households_test.go`:

```go
// before
if recorder.Code != http.StatusUnauthorized {
	t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
}
var body ErrorResponse
if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
	t.Fatalf("decode response: %v", err)
}
if body.Error.Code != "unauthenticated" {
	t.Errorf("error code = %q", body.Error.Code)
}

// after
require.Equal(t, http.StatusUnauthorized, recorder.Code, "body = %s", recorder.Body.String())
var body ErrorResponse
require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
assert.Equal(t, "unauthenticated", body.Error.Code)
```

Style requirements during conversion:

- Conversion is mechanical wherever possible; do not restructure test logic in the same commit. The sanctioned exception is splitting one compound predicate into one assertion per condition: that changes form, not coverage — no condition may be dropped, weakened, or added. Preserve diagnostic context while converting. Broader message revisions, subtest extraction, and test-double restructuring are separate, optional follow-ups.
- Apply the worker-goroutine exception before the mechanical mapping: an error reported with `t.Errorf` and an explicit return inside a worker must not become `require.NoError`. Audit goroutine and helper call sites, not only the assertion's immediate function.
- Preserve comparison semantics and operand types. Go comparisons against untyped constants may need explicitly typed expected values with `assert.Equal`; do not switch to coercive comparisons merely to make converted tests pass.
- Every converted assertion must retain any failure-only context the old message carried (bodies, diffs, IDs) via testify's trailing `msgAndArgs`.
- Replace conditional `errors.Is`/nil-error checks with `ErrorIs`, `Error`, or `NoError`, not an unchanged conditional surrounding `assert.Fail`/`require.Fail`. Specialized matchers must report the expected sentinel as well as the actual error. Reserve `Fail` for unexpected callbacks or impossible states without a suitable matcher.
- Do not delete an assertion because the library "makes it shorter". Coverage stays identical or grows; a compound site that converts to three assertions is three assertions, not one.
- Never weaken the lint configuration to make a conversion pass. `require-error` stays disabled as recorded in decision 9; any further exclusion or settings change is a plan amendment, not a commit-local decision.
- Do not pass a typed nil to `assert.Error`/`require.NoError`. Functions that return a concrete error pointer, such as `validateRegistration` returning `*ValidationError`, wrap a successful nil in a non-nil `error` interface, so the old `if err != nil` semantics convert to `assert.Nil`/`require.NotNil`. Keep `Error`/`NoError` where the function returns the `error` interface.
- Do not pass a response-consuming helper to `msgAndArgs`. Testify evaluates message arguments unconditionally, so `require.Equal(t, want, response.StatusCode, "body = %s", readBody(t, response))` closes the body on the success path and breaks the decode that follows. Where the body is the only useful failure context, keep the two-step form `if !assert.Equal(t, want, response.StatusCode) { t.Fatalf("body = %s", readBody(t, response)) }`; these conditional aborts are deliberate and are not residual hand-written assertions.
- Check a response's gating status before calling `decodeBody`, which consumes and closes the body. A failure-only `readBody` diagnostic must run before decoding, never afterwards.
- Keep `t.Helper()` in all existing helpers.

## 7. Delivery milestones

Each milestone is one or more commits that leave `go vet ./...` and `go test ./...` green. Per `AGENTS.md`, tests for a behavior belong with that behavior, and here the "behavior" is the test code itself, so a milestone's tests and conversion ship together. No commit may leave a failing check: the dependency, the conventions, and the exemplar land together so the first commit is already green and self-documenting.

Ordering principle: introduce the dependency, the rules, and the enforcement in one green commit, then convert low-risk unit packages, then adapters, then integration tests, then record the outcome. One commit per module per test tier, so a reviewer checks one package's style at a time.

### Milestone 1 — Dependency and conventions

- Run `cd backend && go get github.com/stretchr/testify@v1.12.1`, then `mise run tidy` from the repository root. Expect one indirect requirement (`go.yaml.in/yaml/v3`).
- Add `golangci-lint = "2.15.0"` to `mise.toml` and create `backend/.golangci.yml`:

  ```yaml
  version: "2"
  linters:
    default: none
    enable:
      - testifylint
    settings:
      testifylint:
        disable:
          - require-error
  ```

- Extend the `lint` task to four runs: `go vet ./...`, `go vet -tags=integration ./...`, `golangci-lint run ./...`, `golangci-lint run --build-tags=integration ./...`, all under `cd backend`. `check` and `check:all` already depend on `lint`, so CI enforces it without a workflow edit.
- Add the testing conventions to `AGENTS.md`'s "Documentation and testing" section (decision: this is the primary rules home, because that is where a contributor or agent looks during ordinary work), stating the `require`/`assert` rule, the `want`-then-`got` argument order, the worker-goroutine exception, the ban on single-comparison helpers, and that the linter config is amended only through a plan update.
- Add a short "Test conventions" section to `docs/operations.md` pointing at `AGENTS.md` and listing `mise run test`, `mise run test:integration`, and the two lint modes. It has no testing section today.
- Create one converted exemplar file (`internal/config/config_test.go`, 15 sites) to prove the rules and the failure output read well, and use it to confirm the config is neither too strict nor too loose before the mass conversion.
- Acceptance: `go.mod` gains a test-only `testify` requirement; no production code imports it; conventions documented in both files; exemplar converted; all four lint runs report zero issues; `mise run check` green.

### Milestone 2 — Unit tests, no database

One commit per module, in this order. Unit site counts total 180. `internal/auth`, `internal/store`, `internal/reporting`, and `internal/finance` also have integration-tagged files, which get their own commit in milestone 4; that is the intended per-tier split, not a deviation from the module-commit rule.

| Module | Sites | Files |
| --- | --- | --- |
| `internal/config` | 15 | `config_test.go` (exemplar, milestone 1) |
| `internal/logging` | 8 | 1 |
| `internal/store` (non-integration) | 14 | `migrate_test.go` 11, `db_test.go` 3 |
| `internal/web` | 65 | 6 |
| `internal/auth` (non-integration) | 61 | `cookies` 12, `limiter` 10, `middleware` 7, `password` 10, `service` 4, `token` 10, `validation` 8 |
| `internal/reporting` | 11 | `rates_test.go` |
| `internal/finance` | 6 | `money_test.go` |

These modules contain the F2/F3 message defects that matter most (money rounding, rate precedence, session/CSRF rules) and they run fast, so each commit is verified locally without Compose.

Acceptance: no `if got != want` assertion remains in these packages; every converted value comparison reports both expected and actual, while error/setup assertions retain actionable context per decision 5; both lint modes stay at zero issues.

### Milestone 3 — `internal/httpapi`

- 138 sites across 10 files, including the F7 decode/assert duplication. One commit for the module; split by file only if the diff stops being reviewable.
- Convert repeated "status + JSON body" sequences using `require.Equal` for gating status checks; use `assert.Equal` only when status and payload checks are independent. Use `assert.JSONEq` where semantic JSON equality preserves the existing contract; JSON money is already a string (`"amount":"12.34"`), so `JSONEq`'s numeric normalization cannot hide a representation change, but keep typed decoding wherever a numeric field's exact form matters.
- Acceptance: `httpapi` has no hand-written equality assertions; JSON adapter unit tests retain their response-envelope and validation coverage; both lint modes stay at zero issues. HTMX fragment behavior remains covered by `internal/web` in milestone 2 and `internal/server` in milestone 4.

### Milestone 4 — Integration tests

One commit per module; 652 sites total. Order: `internal/server` (275, 10 files) → `internal/households` (129) → `internal/finance` integration (108) → `internal/auth` integration (51) → `internal/reporting` integration (47) → `internal/store/migrate_integration_test.go` (42).

`internal/server` is the largest single commit. If it exceeds roughly 1,500 changed lines, split it by scenario file (auth, entries/entry-HTML, households/household-HTML, rates and reports, MVP smoke) with the tree green in each.

- Requires Compose PostgreSQL, provided by the existing `mise run db:up` and `mise run test:integration` tasks. `AGENTS.md` requires documented commands to stay synchronized with `mise.toml`; if either task changes, update `docs/operations.md` in the same commit.
- Preserve existing scenario/subtest boundaries. Subtest extraction changes fatal-failure scope and potentially cleanup timing, so it must not be folded into conversion commits.
- Audit the 8 concurrent sites for the worker-goroutine exception; preserve explicit returns and synchronization. Known sites: `internal/auth/limiter_test.go` (1), `internal/auth/service_test.go` (1), `internal/reporting/service_integration_test.go` (2), `internal/households/options_integration_test.go` (2), `internal/households/invitations_integration_test.go` (2).
- Acceptance: `go vet -tags=integration ./...` clean, both lint modes (including `--build-tags=integration`) at zero issues, and the full tagged suite green against real PostgreSQL.

### Milestone 5 — Record the outcome

Documentation only; no code change.

- Record before/after assertion and test-case counts as inventory, explaining the delta (compound-predicate splits raise the count; `Len`/`Empty`/`Zero` conversions lower it), plus the list of surviving hand-written assertions with their justification.
- Mark this plan `Complete` with its validation record and set its index row in `docs/plans/README.md` to `Complete` in the same commit.
- Acceptance: the recorded delta is fully accounted for against the 970-site baseline; plan and index agree; all four lint runs and every suite are green at the final commit.

### Deferred follow-up — Test-double hygiene (not an adoption milestone)

F6 is not resolved by assertion adoption. Retain the package-local session doubles during conversion. A shared `internal/testauth` package importing `auth.Session` cannot be imported by the existing `package auth` tests without creating an import cycle. Any later consolidation must explicitly evaluate external-package tests and their access requirements; three small local doubles may remain the simpler solution.

A separately scoped follow-up may standardize household-double behavior on unexpected calls, with explicit diagnostics rather than silent zero values or nil-hook panics. It must account for embedded interface methods as well as hooks, and must not use fatal test assertions from worker goroutines. No double restructuring belongs in conversion commits.

### Deferred follow-up — Broader linter set (owned by the next plan)

This plan enables `testifylint` only. Raising `golangci-lint` to its default set is deferred to the [Backend Review Remediation](./20261114_192841_backend_review_remediation.md) follow-up, in the same commits that fix the findings it surfaces. Verified during preparation so the follow-up does not re-derive it: `golangci-lint 2.15.0` is built with go1.27.0 and typechecks this go1.27.3 module; the default set reports 12 production findings (`errcheck` 6, `staticcheck` 5, `unused` 1, the last being that plan's finding Y1); `expected-actual`, `compares`, and `useless-assert` fire on real converted code; and `require` inside a worker goroutine is not detected even with `go-require` enabled. The tool must run with the mise Go toolchain on `PATH` — outside mise, `GOTOOLCHAIN=auto` can fetch a newer toolchain and fail with `compile: version "go1.27.3" does not match go tool version`.

## 8. Acceptance criteria

- `go.mod` gains `github.com/stretchr/testify` for test imports only; no production package imports it. Do not add `go-cmp` without a separately justified need.
- No `if got != want { t.Errorf(...) }` assertion pattern remains in `backend/**/*_test.go`.
- Value-comparison failures report expected and actual values. Error, setup, and protocol failures retain actionable context without requiring an artificial expected-value message.
- `require` is used for prerequisites and `assert` for independent checks, with no file mixing the two roles arbitrarily.
- Testing conventions are documented in `AGENTS.md` and `docs/operations.md`; `golangci-lint` is pinned in `mise.toml`, and `backend/.golangci.yml` enables `testifylint` only, with `require-error` disabled for the reason recorded in decision 9.
- `go vet ./...`, `go vet -tags=integration ./...`, both `golangci-lint` modes, `mise run test`, and the tagged integration suite are green at the final commit, and at every intermediate commit.
- Every commit is individually green; a module is never left half-converted, and the lint config is never loosened to make a conversion pass.
- Record assertion and test-case counts before/after conversion as inventory and account for the delta: compound-predicate splits increase the site count, `Len`/`Empty`/`Zero` conversions decrease it. Counts do not prove coverage preservation: review predicates, comparison types, expected-error checks, fatal/nonfatal control flow, worker returns, and test-case identity to ensure no coverage was silently dropped.
- Report any surviving hand-written assertions and their justification in the final summary. Setup/protocol failures and worker-goroutine error reporting need not be forced into an expected/actual-value format.

## 9. Risks

- **Review volume.** 970 sites is a large diff, and roughly 150 of them are compound predicates whose conversion splits one `if` into two or three assertions. Mitigation: one commit per module, mechanical mapping, no logic restructuring in the same commit. Only `internal/server` (275) may need splitting further.
- **Argument-order mistakes** (`got, want` swapped) produce confusing diagnostics even when equality results are unchanged. Mitigation: `testifylint`'s heuristic `expected-actual` check, plus review of operand ordering for variable-to-variable comparisons the checker cannot see.
- **Fatal-ness regressions where the old code aborted.** A `t.Fatalf` gate followed by indexing or field access becomes an index panic or nil dereference if converted to `assert`. Mitigation: the downstream-use rule in section 6, and the three canonical cases cited there are checked first.
- **Linter scope erosion.** The narrow config is what keeps every commit green; loosening it mid-migration would hide real regressions, and widening it would surface the follow-up plan's production findings early. Mitigation: config changes require a plan amendment and are never commit-local.
- **Residual gap in enforcement.** `testifylint` does not detect `require` inside a worker goroutine, so decision 5's most safety-critical rule is review-only. Mitigation: the worker rule is stated in `AGENTS.md`, and the 8 known goroutine sites are audited during milestone 4.
- **Concurrency and control-flow regressions.** Fatal assertions in workers and subtest extraction can change failure behavior. Mitigation: prohibit worker `require` calls, preserve explicit worker returns, and defer structural changes.
- **`assert` cascade noise** if prerequisites are converted to `assert` by mistake. Mitigation: the mapping table and the F4 rule are explicit.
- **Integration conversion needs Docker.** A reviewer without Compose cannot verify milestone 4. Mitigation: keep milestone 4 after all unit conversions so earlier milestones remain independently verifiable.
- **Dependency policy objection.** `AGENTS.md` discourages gratuitous libraries. Mitigation: the rationale is recorded in decision 1 and in `AGENTS.md`, and the test-only requirement adds a single indirect module. If review rejects it, the whole plan should be dropped rather than partially applied, because a half-converted suite is worse than either consistent style.

## 10. Resolved and deferred

Resolved at review; nothing blocks execution:

1. **Test-only dependency:** approved. Testify `v1.12.1`, one indirect requirement, no production import.
2. **Lint tooling:** adopted now, narrowly. `golangci-lint` `2.15.0` pinned in `mise.toml`, `backend/.golangci.yml` with `default: none` and `testifylint` only, `require-error` disabled because it conflicts with decision 5, two run modes for the two build-tag sets. This is green before conversion and enforces the new style during it. Widening to the default linter set stays deferred, because its 12 production findings must be fixed where they are introduced.
3. **Land order:** this plan first; the Backend Review Remediation plan is reworked afterwards, writing its new tests in the adopted style and raising the linter set together with those fixes.
4. **Commit granularity:** one commit per module per test tier, with `internal/server` splittable by scenario file.
5. **Conventions home:** `AGENTS.md` holds the rules; `docs/operations.md` holds the commands.
6. **Toolchain pinning:** `golangci-lint` is pinned alongside the existing `go` and `node` pins. The lint task runs through mise, which keeps the pinned Go toolchain on `PATH` and avoids the `GOTOOLCHAIN=auto` version mismatch.

Previously settled, unchanged: `go-cmp` is not part of initial adoption (decision 2); test-double hygiene and `t.Run` extraction are deferred outside this migration (section 4 and the deferred follow-up under section 7). No deferred item blocks completion.

Inventory correction recorded during preparation: `internal/store` has 14 non-integration assertion sites, not 11 — `db_test.go` (3) was missing from the original milestone list. The 970-site total is unaffected because `db_test.go` was already counted in it.

Counting rule for the before/after inventory: the 970 baseline came from `grep -oE '\bt\.(Errorf|Fatalf|Error|Fatal)\('`, which also matches the tail of `assert.Error(` and `require.Error(` at a word boundary. Converted suites must be counted with a pattern that excludes testify calls, for example `grep -oE '(^|[^.[:alnum:]_])t\.(Errorf|Fatalf|Error|Fatal)\('`, with testify call counts reported separately. Otherwise the after-count silently includes every `assert`/`require` line and the delta cannot be accounted for.

## 11. Validation record

Delivered in fourteen commits: one for the dependency, conventions, tooling, and exemplar, one per unit module, one per adapter module, and one per integration module.

| Measure | Before | After |
| --- | --- | --- |
| `_test.go` files | 48 | 48 |
| Test lines | 8,728 | 7,420 |
| Test functions | 177 | 177 |
| `t.Run` sites | 29 | 29 |
| Hand-written assertion sites | 970 | 10 |
| Testify calls | 0 | 1,167 (733 `assert`, 434 `require`) |
| Test-only dependency | none | testify v1.12.1 + one indirect module |

The 187-call increase over 970 sites is accounted for by compound predicates that now report one assertion per condition (for example the 33 sites in `internal/server/households_integration_test.go` that covered two or three conditions each) against the smaller reduction from `Len`/`Empty`/`Zero`/`Contains` matchers replacing loop-and-compare blocks. No assertion condition was dropped: every converted site still checks what its predecessor checked, and the comparison types were preserved (`int32`/`int64` literals, `uuid.UUID`, `time.Time` formatting, `*string` dereferences, and `decimal` via `Equal`).

Review corrections are folded into the original conversion commits: 33 conditional error checks use specialized `ErrorIs`/`Error` matchers instead of `Fail`, preserving the fatal/nonfatal choice and the expected-error diagnostic. The entry PATCH response's status gate runs before decoding, so its failure-only body diagnostic never reads an already-closed body. Assertion counts are unchanged; the specialized matchers and removed unused imports reduce test lines by 41.

The ten surviving hand-written calls are all the same deliberate shape recorded in section 6:

```go
if !assert.Equal(t, http.StatusCreated, response.StatusCode, "create household") {
	t.Fatalf("body:\n%s", readBody(t, response))
}
```

They appear in `internal/server` (`entries_integration_test.go` 3, `households_integration_test.go` 2, `mvp_smoke_integration_test.go` 2, `rates_reports_integration_test.go` 2, `options_invitations_integration_test.go` 1). Testify evaluates `msgAndArgs` eagerly, so passing `readBody` there would consume the body on the success path and break the decode that follows; the conditional abort keeps the body on the failure path only.

Validation performed at every commit: `mise run format:check`, `mise run lint` (both `go vet` modes and both `golangci-lint` modes) at zero issues, `mise run test`, and the tagged integration suite against Compose PostgreSQL. Final state:

```sh
mise run check                        # format, vet, testifylint, unit tests, build
mise run test:integration             # tagged suite against PostgreSQL on localhost:5433
go test -tags=integration ./... -count=1
```

All green. `testifylint` is active and demonstrably fails a reversed expectation (`expected-actual`) and a negated comparison (`compares`), and `.golangci.yml` still enables `testifylint` only, with `require-error` disabled for the reason in decision 9. No test case, subtest boundary, goroutine structure, or cleanup path was changed, with two exceptions recorded in section 6: compound predicates split into per-condition assertions, and typed-nil error checks that needed `Nil`/`NotNil` instead of `NoError`/`Error`.

Follow-ups recorded but not delivered here: the broader linter set, test-double consolidation (F6), and subtest extraction (F5). They belong to the [Backend Review Remediation](./20261114_192841_backend_review_remediation.md) rework and to separately scoped work.
