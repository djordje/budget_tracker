Status: Complete

# Deterministic HTML template formatting

## Context and goal

In [`backend/web/templates/entries.html`](../../backend/web/templates/entries.html), `<div aria-live="polite">`, its child `<div id="entries-content">`, and the child's `<nav>` all start at the same indent level. This obscures the nesting of the persistent live region and the fragment HTMX swaps. Adopt one formatter so source indentation reflects the structure and formatting does not depend on an editor or developer machine.

## Scope and constraints

- Format all tracked `.html` files under `backend/web/templates/`, including shared partials. These are Go `html/template` sources, not standalone HTML documents.
- Use the chosen formatter's tab indentation. Run the same pinned tool locally and in CI.
- Preserve template behavior and rendered output, except for reviewed, insignificant whitespace between elements. In particular, keep visible inline spacing, textarea values, attribute values, and the HTMX swap boundary unchanged.
- Do not sort Tailwind classes, reorder attributes, change template logic, redesign markup, format generated assets, or add a runtime dependency. Keep the existing Go formatting tasks unchanged.

## Proposed approach

1. **Qualify a Go-template-aware formatter.** Start by evaluating Prettier with a Go-template plugin, using the existing Node/npm toolchain. Treat this as a candidate, not a selection: plain HTML parsing is insufficient. Run the candidate against a scratch copy of the entire template directory. Review `entries.html` for both levels of nesting, inline and in-tag conditionals, `{{define}}` blocks, and Go comments; use `_entry_form.html` to check that formatting doesn't add whitespace to the textarea value. Also cover actions with quoted arguments inside HTML attributes and whitespace-trimming markers (use a fixture if needed). Reject candidates that require broad ignores, alter behavior, or fail to produce byte-for-byte identical output on a second pass. If the candidate fails, evaluate another template-aware tool before deciding whether to defer; don't silently fall back to a generic HTML formatter or a regex-based indenter.
2. **Add reproducible write and check commands.** Pin the selected formatter and any plugin as development dependencies with exact versions and commit the lockfile if using npm. Commit the formatter configuration, including explicit parser selection for `.html` templates if needed. Add `mise run format:html` for writes and `mise run format:html:check` for a non-mutating check that reports unformatted paths and fails on drift. Both commands should use the same configuration and template file scope. Document installation and usage in the root [`README.md`](../../README.md).
3. **Apply the baseline and enforce it.** Review the formatting-only diff separately from the tooling changes. Enable the HTML check in both `mise run check` and `mise run check:all` only once the baseline is formatted, so no commit introduces a failing gate. CI already runs `check:all`; use that path instead of a separate workflow. Do not require editor integration.

## Acceptance and validation

- `entries.html` visibly indents `#entries-content` inside the live region and the `<nav>` inside `#entries-content`, with matching alignment for closing tags. In the rendered page the live region remains outside the swapped fragment, and the normal and error responses retain the expected `#entries-content` target.
- From clean dependencies (`mise run deps` for the npm approach), `mise run format:html` is idempotent and `mise run format:html:check` passes on the baseline. Deliberately misindent a template in a scratch copy and verify the check fails, identifies the path, and leaves the file unchanged; the write command should restore compliance.
- Compare representative before/after renders for populated and empty entries, scoped filters, HTMX responses, and a nonempty entry description. Review inline text, options and conditional attributes in both branches, and exact textarea contents; do not globally normalize whitespace in this comparison.
- Run `mise run check:all` with local PostgreSQL to cover template parsing, HTML/HTMX behavior, linting, and the build. Record the selected tool, any exceptions, and validation results here before marking the plan complete.

## Implementation and validation record

- Selected `github.com/gohugoio/gotmplfmt` v0.5.0 after testing all 18 templates. Its built-in style uses tabs, normalizes Go action spacing, and does not reflow long tags. No formatter options, ignore directives, or source workarounds are needed. The Prettier 3.10.1 trial with `prettier-plugin-go-template` 0.0.15 added a newline before the description and was not idempotent with either default or strict HTML whitespace settings. djLint 1.36.4 was idempotent but lost the outer section indentation after the multiline Go comment in `entries.html` and reflowed considerably more markup.
- Pinned `gotmplfmt` in [`mise.toml`](../../mise.toml), added the two HTML formatting tasks, and enabled the check in both aggregate tasks with the formatted baseline. The tool is installed by `mise install`, not added to the application module or npm dependencies.
- Compared scratch before/after renders of populated entries, both empty states, both scope selections, the HTMX error fragment, the entry form with a multiline description, and an action fixture with whitespace trimming. Tags, attributes, and text matched apart from indentation after newlines outside the textarea; the raw textarea content matched exactly. The live region still wraps the normal swap target and is absent from the error fragment.
- Verified `mise run deps`, the scratch drift/non-mutation check, and byte-for-byte idempotence after reformatting. `mise run check` and `mise run check:all` passed; the latter ran with local PostgreSQL, including both lint modes, unit and integration tests, and the build.
