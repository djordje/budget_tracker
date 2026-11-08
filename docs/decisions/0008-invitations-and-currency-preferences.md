# 0008: Invitation links and inherited currency preferences

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

The MVP has no email delivery provider. Invitations still need to be secure,
single-use, and restricted to the intended email address. Currency defaults
also need household-level values and optional personal overrides without
coupling finance and reporting to presentation code.

## Decision

- Generate a random invitation token and store only its SHA-256 hash. Return
  the raw token once so an admin can share the `/invitations/{token}` link
  manually until email delivery is added.
- Invitations expire after seven days, can be accepted once, and require the
  signed-in account's normalized email to match the invitation email.
- Only household admins can create, list, or revoke invitations. Acceptance
  creates the household membership with the invited role and sets that
  household active.
- Pending invitations are unique per household and normalized email. Expired
  pending rows are replaced when a new invitation is created.
- Store household currency defaults in `household_options` and per-user,
  household-specific overrides in `user_options`. Null override fields inherit
  the household value.
- Only admins can change household defaults; each member can change their own
  overrides. Entry defaults and report display defaults resolve the effective
  values in the application services.
- Redact invitation tokens from request logs.

## Consequences

- A copied invitation link is the delivery mechanism in the MVP; adding email
  delivery later does not change the token or acceptance model.
- Invitation tokens are never stored in plaintext and are not logged.
- Preferences remain isolated per household for users who belong to several.
- Finance and reporting behavior is consistent across HTML and JSON adapters.

## UI reuse

Repeated styling is centralized in semantic Tailwind `@layer components`
primitives for buttons, controls, labels, errors, cards, headings, alerts, and
muted text. Genuinely shared error, field-error, entry empty-state, and
exchange-rate form markup lives in flat partials with typed view models.
These changes preserve appearance and behavior; they do not introduce a
frontend framework, rendered-HTML slots, or a generic template dictionary.
