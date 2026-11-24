Status: Complete

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
- Run `mise run format:check`, `mise run format:html:check`, `mise run lint`, `mise run test`, and `mise run test:integration`. Smoke-test household creation with a non-RSD currency, entry creation and invalid submission on mobile, and the new rate form.

## Delivered

1. `households.CreateAndActivate` takes `CreateParams` with an optional `CurrencyCode`, validates it against the catalog, and seeds all three defaults in the creation transaction. The HTML form requires a visible choice and re-renders its fields on errors; the JSON adapter keeps the RSD fallback when `currency_code` is omitted.
2. `finance.RecentCurrencies` returns the actor's own recently recorded codes in one household within the 20-entry window, and `finance.OrderCurrencies` orders entry-form choices: selection first, then recents, then the catalog. Both entry-form handlers use it for new, edit, and validation renders.
3. The new-rate form no longer preselects EUR → RSD, shows a "Choose a currency" prompt for both currencies, and marks the pair, rate, and date as required.

## Validation

- `mise run format:check`, `mise run format:html:check`, `mise run lint`, `mise run test`, and `mise run test:integration` pass, including new unit, service integration, and server HTML integration tests. The HTML integration tests cover household creation with a non-RSD currency, entry creation, invalid entry submission, and the new-rate form; no manual mobile or browser smoke test was performed in this session.
- Covered by tests: EUR creation seeds all three defaults, omitted JSON `currency_code` still yields RSD, unknown, blank, null, and non-string codes return a `currency_code` field error without creating a household, a padded lowercase code is accepted and normalized, recents exclude other members' and household-wide entries, recents separate by household, the 20-entry window drops older codes, backdated entries order by recording time, repeated codes collapse, and edit and validation renders keep the selected currency.
- `EXPLAIN (ANALYZE, BUFFERS)` on synthetic data (65k entries for the actor plus 40k unrelated rows): the baseline scanned the actor's full history and top-N sorted it, about 17 ms. Migration `0010_recent_entry_currency_indexes` adds `(household_id, user_id, created_at, id)` indexes to both entry tables, giving a bounded index scan of about 0.1 ms on the same data.
- Write cost measured with `pgbench` single-row inserts and bulk inserts on the same tables. Single-row throughput with `synchronous_commit` off overlapped between 14.2k and 15.4k transactions per second with and without the index, so commit overhead hides most of the difference; a 200k-row bulk insert isolates it at 2.44 s with the index against 2.25-2.34 s without, roughly 1-2 µs per row. At 500k rows the index occupies 33 MB, about 63% of the 52 MB table. The indexed columns never change after insert, so entry edits keep their previous HOT eligibility.
- The DESC variant measured 93 MB versus 33 MB for ascending keys on the same 500k-row synthetic workload. For a fixed `(household_id, user_id)`, ascending timestamp and ID keys place newly recorded entries at the end of that key range, not necessarily at the right edge of the whole index. The page-split and size benefits are workload-dependent. `ORDER BY created_at DESC, id DESC` can still use a backward scan of the ascending index within one household and user. Ascending keys remain the choice because both directions serve the query and the append advantage simply moves to the opposite end of the whole index for a DESC index; changing it again needs a measurement where the household's range is not last.

## Open questions

- None. The 20-entry window is confirmed, and the synthetic-data measurements decided the index question in favor of migration `0010`.
