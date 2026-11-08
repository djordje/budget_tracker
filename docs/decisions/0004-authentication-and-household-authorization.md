# 0004: Separate authentication data and household-scoped authorization

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Users can belong to multiple households with different roles, and authentication
details must not leak into the public user profile. Every household-owned query
needs an explicit authorization check rather than trusting a client-supplied ID.

## Decision

- Keep the public `users` table free of credentials and provider secrets.
- Store normalized, unique login emails and password hashes in
  `auth_credentials`, isolated from the user profile so authentication can be
  replaced later.
- Store only hashes of opaque session and invitation tokens. Use secure,
  HTTP-only, SameSite cookies and CSRF protection for cookie-authenticated
  mutations.
- Scope household-owned queries by `household_id` and enforce membership and
  role rules in application services shared by HTML and JSON handlers.
- Use household-specific roles: admins manage household settings, invitations,
  household-wide entries, and all-entry visibility; members manage their own
  entries.

## Consequences

- Cross-household access attempts require explicit authorization tests for every
  household resource.
- An entry's attribution is separate from the household role that permits
  editing it.
- External identity providers can be added through a future `auth_identities`
  table without changing household or finance tables.
