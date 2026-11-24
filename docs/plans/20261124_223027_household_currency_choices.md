Status: Proposed

# Household currency choices

## Context and goal

[`0004_currencies.sql`](../../backend/db/migrations/0004_currencies.sql) seeds a shared reference catalog of nine currencies. Alpha-3 codes are foreign keys for entries, rate pairs, and currency defaults. The catalog's minor-unit exponents determine how stored amounts are interpreted. For example, ISO 4217 and the database use two digits for RSD, while `Standard.Rounding` in the project's pinned `golang.org/x/text/currency` returns zero. The global catalog is appropriate; treating it as every household's preferred list is not.

New households currently inherit RSD for all three defaults from [`0003_households.sql`](../../backend/db/migrations/0003_households.sql). Entry forms list the shared catalog, and the new rate form prefers EUR → RSD. Rates themselves are already household-owned: [`0006_exchange_rates.sql`](../../backend/db/migrations/0006_exchange_rates.sql) scopes uniqueness by household, and [reporting](../../backend/internal/reporting/service.go) loads that household's rates only. This follows the [MVP baseline](./20261108_201136_budget_tracker_mvp_plan.md).

Make the existing catalog useful to households with different preferences, without expanding it or changing currency or exchange-rate storage.

## Scope and design

- **Keep the catalog unchanged.** Do not edit applied migrations, add or remove supported codes, change exponents, or introduce a currency library. Keep `/api/v1/currencies` as the shared catalog. Do not add household allowlists, favorites, or a persisted recents list.
- **Choose an initial currency at household creation.** Offer the existing catalog on the new-household form. Pass the choice to the household service, validate it against the catalog, and explicitly write it to all three defaults in the existing creation transaction. Defaults remain independently editable; existing households and user overrides are untouched. For JSON creation, add an optional `currency_code`: omission preserves the existing RSD behavior for current clients; an explicitly invalid value returns a field error. An explicit `null`, a blank string, a non-string value, and an unknown code all count as invalid. HTML should require a visible choice rather than silently assuming RSD.
- **Order entry currency choices by relevance.** Keep the effective income or spending default visibly preselected on a new form. Show it first, then currencies from the actor's own recent entries in the active household, then the rest of the catalog in code order. Deduplicate across groups. A recent currency never replaces the default automatically. On edit or validation re-render, preserve the applicable existing or submitted selection rather than resetting to the default. Keep every supported currency available, even with no history.
- **Derive recents from entries.** In `finance`, scope by the authenticated actor and household, check membership, and exclude other users' and household-wide entries even for admins. For both entry kinds, take the latest 20 recorded entries across incomes and spendings before deduplicating, using `created_at` rather than `entry_date` and a stable tie-breaker across tables. One implementation is to select up to 20 rows per table, combine with `UNION ALL`, then apply the final order and limit. Order the resulting codes by most recent use. No recents query on successful saves or per keystroke; build suggestions only for rendered entry forms, including validation responses. Do not personalize rate, settings, or report pickers in this pass.
- **Stop preselecting EUR → RSD for everyone.** Leave the new rate pair unselected with clear required fields, and preserve submitted values on errors. Keep the existing household ownership, permissions, exact-date and monthly lookup, and missing-rate behavior. Shared provider rates are out of scope.

## Delivery milestones

1. Add and test initial-currency handling in the household service, then update the JSON and HTML creation adapters. Preserve API behavior when the field is omitted and keep each step buildable.
2. Add and test the scoped recent-currency query and its ordering in the finance service. Wire it into the server-rendered entry forms, including HTMX validation responses, without changing the catalog endpoint or entry write API.
3. Remove the fixed new-rate pair and update form validation and UI tests. Check the household-specific rate behavior remains unchanged.

## Acceptance and validation

- Creating an EUR household initializes all three defaults to EUR; they can later be edited separately. Missing HTML choices and unknown codes produce field errors without creating or activating a household. Omitted JSON `currency_code` still initializes RSD.
- A new entry still needs only its amount when defaults are available. New users see the effective default and the complete catalog. Backdated entries count by recording time; repeated recent codes appear once; the same user sees different recents in different households. Other members' and household-wide entries do not influence suggestions. Edit and HTML/HTMX validation paths preserve selected currencies.
- Rate creation opens without a preselected pair. Two households can keep different rates for the same pair and date, and each report uses only its own rates.
- Check the recent-entry query with `EXPLAIN (ANALYZE, BUFFERS)` on PostgreSQL. There is no live deployment or representative dataset; seed synthetic data in a development or test database as needed, including long histories for the actor and unrelated rows from other users and households. Existing indexes scope by `(household_id, user_id)` but order by `entry_date`, so `LIMIT` does not guarantee a bounded scan by `created_at`. Start without new indexes. If the baseline warrants investigation, compare plans and latency with `(household_id, user_id, created_at DESC, id DESC)` in the test database before deciding whether to add a separate migration.
- Run `mise run format:check`, `mise run format:html:check`, `mise run lint`, `mise run test`, and `mise run test:integration`. Smoke-test household creation with a non-RSD currency, entry creation and invalid submission on mobile, and the new rate form. No implementation checks have been run for this proposal.

## Open questions

- The 20-entry window is confirmed. The remaining performance decision is whether the recents query warrants a new index; decide from synthetic-data measurements during implementation.
