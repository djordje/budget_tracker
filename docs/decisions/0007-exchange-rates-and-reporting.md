# 0007: Household exchange rates and incomplete report semantics

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Reports must combine entries in several currencies without floating-point
error, ambiguous rate selection, or silently dropping amounts. Rates are
household-specific and need exact-date and monthly scopes while remaining
explainable to users.

## Decision

- Store rates with a declared decimal precision (`numeric(18,8)`), require
  positive values, and reject more than eight fractional digits at the service
  boundary.
- Normalize monthly `rate_date` values to the first day of their month. A
  monthly rate applies to every entry within that calendar month; an exact-date
  rate applies only on that exact date.
- Resolve a conversion using, in order: direct exact-date rate, inverse
  exact-date rate, direct monthly rate, inverse monthly rate. Never chain
  through a third currency.
- Compute inverse rates from the stored rate at lookup time rather than
  storing a prematurely rounded reciprocal.
- Convert with exact rational arithmetic. Round half-even only when producing
  the display currency's minor-unit precision.
- Keep all report totals in one display currency. Same-currency entries do not
  need a rate.
- When a rate is missing, do not treat the entry as zero or omit it silently.
  Exclude it from converted totals, mark the report incomplete, and list the
  missing currency pair and entry date.
- A conversion or total that overflows `int64` aborts the report with an error
  instead of a partial summary. An unrepresentable amount is wrong, not merely
  missing a rate.
- “Mine” excludes household-wide entries. “All” includes every personal entry
  and household-wide entry and remains admin-only.
- Any household member may maintain household exchange rates. Rate IDs are
  positive bigint identity values, exposed as decimal strings and parsed from
  routes as positive base-10 integers.
- A report summary reads display options, currency metadata, entries, and
  exchange rates in one read-only `REPEATABLE READ` transaction, so every
  response represents a single database snapshot.

## Consequences

- Reports can be partial but always state why and which rates are missing.
- Rate precedence is deterministic and directly covered by unit and
  integration tests.
- The display currency can differ from every entry currency without mixing
  units before conversion.
- Concurrent entry, rate, or option writes cannot produce a summary that mixes
  values from different snapshots.
- Chained and rolling-window conversion remain deferred.
