# 0009: Browser security headers and login throttling

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

The application serves cookie-authenticated HTML and JSON from one executable.
It needs conservative browser defaults without requiring a separate frontend
build pipeline, and failed login attempts need basic throttling before local
handoff.

## Decision

- Serve a Content Security Policy that only allows same-origin scripts, styles,
  images, fonts, and connections. Inline event handlers are not used; a small
  external `app.js` handles confirmation prompts and HTMX is loaded from a
  local asset.
- Set `X-Content-Type-Options`, frame denial, a no-referrer policy, a
  restrictive permissions policy, same-origin opener/resource policies, and
  `Cache-Control: no-store` for non-static responses.
- Enable HSTS only when the server is configured for production cookies.
- Throttle failed logins in process at five attempts per IP/email pair per
  fifteen minutes, returning a retry delay.
- Redact invitation tokens from request logs.

## Consequences

- The browser cannot execute inline injected script, and invitation tokens do
  not leak through referrers or request logs.
- The login throttle is intentionally simple and single-instance. A
  multi-instance deployment must replace it with a shared limiter.
- HTMX features that depend on eval or injected indicator styles are disabled.
- Security headers apply consistently to HTML, JSON, health, and static
  responses.
