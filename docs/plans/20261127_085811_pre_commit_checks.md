Status: Proposed

# Fast checks on the commit Git will record

`AGENTS.md` requires every commit to work independently. CI already applies migrations and runs `mise run check:all` on pushes and pull requests, but it doesn't give feedback before a local commit. Add a local gate for common breakage, not a second CI environment. A local hook is bypassable; passing it is not proof that migrations, PostgreSQL behavior, or the release build work.

## Local measurements

Baseline: `4da4b4d`, macOS arm64, configured mise tools and dependencies installed, existing compiler/linter caches. Three sequential wall-clock samples per command (seconds); no cache clearing. The Go test measurements used `mise exec` and added `-count=1` to the task's Go command so a cached pass would not hide execution time.

| Candidate | Run 1 | Run 2 | Run 3 | Proposed gate |
| --- | ---: | ---: | ---: | --- |
| `format:check` | 0.08 | 0.04 | 0.04 | Local + CI |
| `format:html:check` | 0.03 | 0.02 | 0.02 | Local + CI |
| `lint` (both build tags) | 1.62 | 1.25 | 1.25 | Local + CI |
| `test` (unit) | 1.40 | 0.72 | 0.74 | Local + CI |
| `test:integration` (includes unit tests) | 10.70 | 10.77 | 10.87 | Full check only |
| `build` (includes npm assets) | 1.07 | 0.46 | 0.45 | Full build outside hook |

Postgres was already healthy: those times exclude Docker startup. Don't probe for Docker or conditionally skip tests based on whether it's running. The release build is fast in a prepared **working tree**, but a staged-only checkout has no `node_modules`. Don't install or borrow JS dependencies for every commit. The unit and lint checks already compile Go packages; CI should still build the actual asset-bearing executable. Developers should run relevant integration/build checks before committing changes that need them.

## Proposed implementation

1. Add one executable POSIX shell script, `.githooks/pre-commit`. Fail on error. From the repository root, export Git's effective index (honor `GIT_INDEX_FILE` if supplied) via `git checkout-index --all --prefix="$tmp/"` to an absolute, private `mktemp` directory, with cleanup on exit/interruption. Run the existing `mise run format:check`, `format:html:check`, `lint`, and `test` tasks sequentially there, stopping on the first failure. Show task output. No formatter writes, stashing, restaging, glob-based change routing, hook framework, or new CI jobs. Run the same gate on docs-only commits initially.
2. Run mise against the exported **staged** configuration, not the original working directory. Account for mise trust explicitly: installation opts into executing this repository's staged tasks; scope `MISE_TRUSTED_CONFIG_PATHS` to the fresh export for the child commands only. Do not change global trust settings. The export needs neither `.env` nor ignored/generated assets. With the current tasks, an index export plus all four commands passed in **3.52 / 3.30 / 3.24 s** (fresh temp directory each time, normal Go caching). These are prototype timings, not installed-hook timings. Document the snapshot guarantee precisely: it is a Git checkout of the effective index, not a raw copy of staged blobs. Checkout attributes and configured filters can transform the exported bytes, even via an unstaged `.gitattributes`. Accept normal Git checkout semantics rather than building a custom blob exporter.
3. Add an explicit `mise run hooks:install` task in `mise.toml`; don't invoke it from `setup`. If the effective `core.hooksPath` value is exactly `.githooks`, succeed without changes. If any other value is configured (including an inherited or empty value), stop and identify the conflict without changing anything. Otherwise run `git config --local core.hooksPath .githooks`. Git then runs the tracked hook directly; nothing is copied or symlinked. Document that enabling the hook replaces hooks Git would otherwise run from `.git/hooks`; the task does not scan that directory.
4. Document `mise run hooks:install` as the one-time opt-in in `README.md`, along with prerequisites, what runs, how to debug a failure, and how to undo installation (`git config --local --unset core.hooksPath`). Leave `AGENTS.md` unchanged. Its commit rules still apply; keep hook setup and troubleshooting in `README.md` only. Keep `check`, `check:all`, and `.github/workflows/ci.yml` unchanged; do not add a binary smoke check.

## Delivery and acceptance

Deliver the hook, installation docs, and validation together. Exercise actual `git commit` in a disposable clone with tooling installed:

- Valid staged tree commits without Docker, Node dependencies, or `.env`. A staged formatting error or failing unit test blocks the commit with actionable output.
- Stage broken Go, then fix only the working copy: commit must fail. Reverse the case: valid staged code with an unstaged break must pass. Compare index and working files before/after the hook; no changes or leftover export on failure.
- Stage a Go compile error, then edit `README.md` and run `git commit --only -m docs -- README.md`. The commit must succeed and contain only the README change. The Go error must remain staged and block the next ordinary commit. This exercises Git's alternate commit index, not just partial staging.
- Verify installation in a fresh clone, repeat installation as a no-op, and removal without changing global config. Verify that a conflicting inherited or local hooks path causes the installer to refuse without changing config or hook files. Confirm that installation leaves any default hook files untouched but Git stops running them while `.githooks` is enabled.
- Time the installed hook end to end through actual `git commit` invocations, using a different valid staged input each time (include Go and HTML template edits). Report a fresh-cache run separately from at least three warm runs; don't benchmark repeated checks of an unchanged index. Proposed ordinary-commit budget: median **under 5 seconds** on the measured machine (a selection criterion, not a timeout). Record actual hook timings here before marking complete. If over budget, measure which step dominates before dropping anything; don't split the lint modes or introduce path filters by default.

Decision for review: is the measured ~3–4 seconds acceptable on every commit? Cold caches and slower machines may cost more. No performance claim for those environments yet.
