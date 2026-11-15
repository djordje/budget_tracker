# Backend Review Remediation Follow-ups

Status: In progress — accepted from the post-delivery reviewer panel on 2026-11-15. This plan owns the review findings against the completed [Backend Review Remediation](./20261114_192841_backend_review_remediation.md); that plan stays read-only and keeps its `Complete` status.

## 1. Context

The remediation plan was delivered as commits `40d8027..d18c768` (35 commits, milestones 0–7) and verified: every commit builds, vets in both build-tag modes, is `gofmt`-clean, and passes unit tests; `mise run check:all` passes at the tip; the widened lint probe reports zero issues in both modes.

Four independent reviewers then examined the security fixes, the web/HTTP adapter behavior, the new tests, and the domain/schema/docs work. No P0 or P1 issue was found. They raised the P2 findings below: adapter-consistency gaps, two places where a recorded claim overstates or misstates what shipped, three test-strength gaps, one operator-facing documentation error, and one availability consequence of the chosen limiter design that belongs in the record.

The review also noted that the working tree was moving during review; that was the supervising session checking out each commit to verify buildability, not a concurrent writer.

## 2. Scope

Fix the accepted findings below with small commits that leave the tree formatted, linted in both build-tag modes, and tested. Preserve the product rules and the decisions recorded in the completed plan (Q1–Q7); this plan changes no product behavior except where a fix below explicitly says so.

Non-goals: no new features, no change to money, rate precedence, attribution, or authorization rules, no CSRF hardening beyond consistency (Q6 stands), no rewrite of the completed plan or of `docs/plans/20261114_191415_test_assertion_library_adoption.md`.

## 3. Accepted findings

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| R1 | P2 | `web.logPath` and `httpapi.RequestLogger` test a literal `/invitations/` prefix, so a non-canonical path the mux cleans to a live invitation URL (`/x/../invitations/<token>`, `//invitations/<token>`) is logged with the token. | Clean the path before the prefix test in both helpers; add table cases. |
| R2 | P2 | HTML `parsePage` rejects `page=0` with 400 while the JSON adapter treats `0` as "use default", against §6's adapter-agreement criterion. | Treat `0` as the first page in the HTML adapter, matching the recorded Q/C5 decision; keep negatives at 400 and pin both in tests. |
| R3 | P2 | An oversized HTML form without a declared `Content-Length` surfaces as a plain-text CSRF 403 (or a handler 400) instead of 413, while JSON reports `request_too_large`. | Parse the form explicitly in `CSRF.Protect` and map `*http.MaxBytesError` to 413; keep other parse failures at 403. Test a chunked oversized urlencoded body. |
| R4 | P2 | `renderSettingsError` still maps `auth.ErrSessionNotFound`, which no caller can produce after the Y6 change. | Drop the dead case. |
| R5 | P2 | The Y6 invariant ("every de-redundified handler sits behind `RequireAuth`") is asserted by a test that builds its own wrapper chain, so a missing wrapper in `server.go` would go unnoticed. | Add one server-level table test that sends anonymous requests to every `/app…` route and `POST /logout` and expects `303` to `/login`. |
| R6 | P2 | HTMX entry-delete errors render the fragment with 404/403/500, which HTMX does not swap, so the user sees nothing; the entry-form HTMX path deliberately renders 200 fragments. | Render HTMX delete-error fragments with 200 like `renderEntryForm`, keeping the real status for non-HTMX responses. |
| R7 | P2 | `ListInvitations` still says it lists "pending" invitations although expired rows are intentionally listed. | Reword the comment. |
| R8 | P2 | The per-IP admission budget of 10 new buckets per window can lock out the 11th distinct account behind one NAT/CGNAT for 15 minutes; registration also spends it before the `password_confirm` check, so typos consume budget. | Raise the budget to a documented 50 new buckets per window and only acquire the registration bucket when the form's local checks pass; keep acquisition before any Argon2 work. Pin the policy constants in tests. |
| R9 | P2 | The fail-closed key cap (refusing new buckets at 10,000) can deny new logins/registrations if a distributed client set fills the cap. This is the accepted §Milestone 1 alternative but is absent from the record. | Record it in this plan's delivered-record section; no code change. |
| R10 | P2 | The `safeNextPath` "backslash authority" case asserts only generic properties, so replacing the `RequestURI()` escaping with the raw value would keep the test green while reintroducing a cross-origin redirect. | Assert the escaping mechanism (`/` and `%5C`, no raw backslash). |
| R11 | P2 | No test proves a single admission key recovers its budget after the window, and the S4 policy constants (`limiterMaxKeys`, `limiterMaxKeysPerAdmission`) are never asserted. | Add the window-recovery case and pin the constants. |
| R12 | P2 | `docs/operations.md` still documents oldest-key eviction and omits the registration throttle and the HTML body bound. | Rewrite the two bullets to match the shipped behavior. |
| R13 | P2 | §11 of the completed plan claims all three date parsers were consolidated, but `web.parseFormDate` still parses on its own; the shared parser also silently made the JSON API accept whitespace-padded date parameters, which Q7 did not allow. | Make `parseFormDate` delegate to the shared parser and make that parser strict (no trimming), restoring the API's original strictness; correct the §11 count and record the strictness choice here. |
| R14 | P2 | The `-race` limiter command recorded in §11 uses an escaped `\|`, which matches no test, so the recorded evidence does not support the claim. | Correct the command in §11 (factual record fix, allowed by the plans README), re-run it unescaped, and record the result here. |

Report-only, deliberately not changed: `basePageData.Year` uses host local time for the footer year (not a user-visible default date under C4); the migration's `NOT VALID`/`VALIDATE` split does not shorten the lock window under this runner, which executes a migration file in one transaction — the migration comment is corrected to say so honestly, and the split is kept as the standard pattern.

## 4. Delivery record

Filled in when the fixes land.

## 5. Acceptance

- `mise run check:all` passes; every commit is `gofmt`-clean, vet-clean, and lint-clean in both build-tag modes.
- Each accepted finding is fixed by a test that fails without the fix, or is explicitly recorded as a deliberate no-change.
- `docs/operations.md` matches the shipped limiter, body-limit, and throttle behavior.
- The completed remediation plan keeps its `Complete` status; only its demonstrably wrong validation command and consolidation count are corrected.
