# 0006: Entry attribution, visibility, and modification

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

The plan distinguishes personal entries from household-wide entries and
preserves “my data” separately. It also states that only admins can view all
entries and modify household-wide entries, while members manage their own
entries. The creation and edge-case rules needed a concrete interpretation.

## Decision

- Personal entries belong to one user and are visible to that user. Admins may
  view every personal entry in the household.
- Household-wide entries (`user_id = NULL`) are visible only to admins.
- Members may create only personal entries attributed to themselves.
- Only admins may create, edit, or delete household-wide entries.
- An owner may edit or delete their own personal entry. An admin may not edit or
  delete another member’s personal entry; they can view it through the
  household-wide scope.
- Attribution is fixed after creation. Updates can change amount, currency,
  date, and description, but not ownership.
- “Mine” always excludes household-wide entries. “All” includes all personal
  entries and household-wide entries and is available only to admins.
- Entry IDs are positive bigint identity values. JSON exposes them as decimal
  strings, and routes parse only positive base-10 integers.
- The finance package owns one entry view/modify policy. Services enforce it,
  and HTML adapters use the same policy for button and edit-form visibility
  rather than reimplementing the rules.

## Consequences

- Members cannot accidentally create a shared entry they cannot later see or
  edit.
- Admin visibility and edit rights are intentionally different: admins audit
  household data without impersonating members.
- The API and HTML adapters share these rules through the finance service and
  its entry policy.
- Cross-household and role-boundary cases require explicit tests.
