# 0003: Integer minor units for money and exact decimals for exchange rates

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Floating-point money and rates cause subtle, compounding report errors. The
application must support currencies with different minor-unit exponents and
round only when producing a displayed amount.

## Decision

- Parse money from decimal strings into positive `bigint` minor units in Go and
  PostgreSQL, alongside the currency code.
- Store each currency's ISO 4217 alphabetic code and minor-unit exponent in the
  `currencies` table. Never assume two decimal places.
- Store exchange rates as exact decimals using PostgreSQL `numeric` and an exact
  decimal type in Go.
- Convert with exact intermediate arithmetic. Apply half-even rounding only when
  producing the display currency's minor-unit precision.
- Resolve rates by exact date before monthly scope, and direct before inferred
  inverse, without chained conversion.
- Mark reports incomplete and identify missing rates instead of omitting entries
  or treating them as zero.

## Consequences

- Money and rate handling require table-driven tests at the domain boundaries.
- Entry amounts and original currencies are preserved; conversion happens at
  report time using the entry date.
- Reports must carry conversion warnings and incomplete status.
