# 0005: Argon2id credentials, opaque sessions, and double-submit CSRF

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

The application uses local email/password authentication and cookie-backed
sessions. Sessions must be revocable, credentials must be isolated from public
profiles, and cookie-authenticated mutations need CSRF protection without
introducing a session dependency for anonymous login and registration forms.

## Decision

- Hash passwords with Argon2id using a random per-password salt and current
  parameters recorded in the encoded hash. Compare hashes in constant time.
- Normalize emails by trimming and lowercasing, then enforce uniqueness in
  `auth_credentials`. Keep credentials out of the public `users` table.
- Issue a random raw session token in an HTTP-only, SameSite=Lax cookie. Store
  only its SHA-256 hash in PostgreSQL and use that hash as the session primary
  key; sessions have no separate UUID. Sessions expire after 30 days and are
  revoked by deleting the row.
- Remove expired sessions with a bounded opportunistic cleanup after a
  successful registration or login. Cleanup runs at most once per hour per
  process, and a cleanup failure is logged without invalidating the session
  that triggered it.
- Set the Secure flag on session and CSRF cookies in production.
- Protect unsafe requests with a double-submit CSRF token. The token is
  readable by scripts in a SameSite=Lax cookie and must be submitted in a form
  field or as `X-CSRF-Token` for JSON requests.
- Store the selected active household on the session row; membership is checked
  again when it changes. Session updates address the row by token hash.
- Create a household, its initial admin membership, default options, and the
  session's active-household preference in one transaction. A failed
  activation rolls back the household so a retry cannot leave a partial
  record.

## Consequences

- Session tokens and passwords are never stored in plaintext.
- Anonymous login and registration forms receive CSRF protection without
  creating a session first.
- API clients using cookie authentication must echo the CSRF token for unsafe
  requests.
- Login throttling and broader request rate limiting remain hardening work for
  the final milestone.
