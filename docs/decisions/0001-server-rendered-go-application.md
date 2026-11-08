# 0001: One Go executable with server-rendered HTML and a versioned JSON API

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

The MVP needs to work well on mobile and desktop, support multi-user household
workflows, and leave room for future native clients, integrations, and an MCP
server. It is also a Go learning project, so the implementation should expose
application behavior instead of hiding it behind a framework.

## Decision

Build one Go executable that serves:

- server-rendered HTML under `/app`, progressively enhanced with HTMX;
- versioned JSON endpoints under `/api/v1`;
- static assets used by the HTML interface.

Use the standard-library `net/http` router, `html/template`, and `log/slog`.
Tailwind CSS is compiled at build time. HTML and JSON handlers are thin
adapters over shared application services, and services own business rules,
transactions, and authorization.

## Consequences

- There is no SPA or separate frontend project; browser interactivity stays
  small and progressive.
- Every operation needed by both interfaces must live in a service rather than
  in an HTTP handler.
- Templates and static assets are built and embedded into the executable.
- The JSON API can evolve independently while reusing the same services.
- Adding a client or MCP tools later does not require duplicating domain rules.
