# Plans

This directory contains implementation plans that should remain in the repository and provide enough context for future development sessions.

## Naming convention

Create each new plan as:

```text
<YYYYMMDD_HHMMSS>_<short_title>.md
```

- Use the creation timestamp, in UTC, so filenames sort chronologically.
- Use a short, descriptive, lowercase `snake_case` title.
- Update an active plan in place as decisions change; do not create timestamped copies for ordinary revisions.
- Create a new plan for a distinct initiative or a superseding plan, and link between related plans when useful.

## Plan contents

Keep plans self-contained so a future session can resume work without relying on chat history. Start every new plan with a `Status:` line from the lifecycle vocabulary below; existing plans without one take their status from the index. Include relevant context, goals, scope and non-goals, confirmed decisions, proposed architecture, delivery milestones, acceptance criteria, and unresolved questions.

## Status and lifecycle

- **Proposed** — written for review; not approved or started.
- **In progress** — approved; the plan is the active reference and changes in place as work proceeds.
- **Complete** — delivered and validated; the plan is now a historical record.
- **Superseded** — replaced by a newer plan; link in both directions and mark the newer plan as its successor.

Rules:

- Amend `Proposed` and `In progress` plans in place; they are living documents.
- Treat `Complete` and `Superseded` plans as read-only history. Do not rewrite their milestone tables, acceptance criteria, confirmed decisions, or recorded validation results — including marking a delivered milestone incomplete because a later review found a gap.
- Record new findings, regressions, and unmet acceptance in the active plan, or in a new plan that links to the completed one. The completed plan stays unchanged and keeps its `Complete` index status; the follow-up plan owns the work.
- Correcting a broken link, typo, or factual error in the record is acceptable. Changing what was delivered or how it was validated is not.
- Keep each index row's status synchronized with the plan header.

## Index

| Created | Plan | Status |
| --- | --- | --- |
| 2026-11-08 | [Budget Tracker MVP](./20261108_201136_budget_tracker_mvp_plan.md) | Initial implementation baseline |
| 2026-11-13 | [Code Review Remediation](./20261113_163019_code_review_remediation_plan.md) | Complete |
| 2026-11-14 | [Test Assertion Library Adoption](./20261114_191415_test_assertion_library_adoption.md) | Complete |
| 2026-11-14 | [Backend Review Remediation](./20261114_192841_backend_review_remediation.md) | Complete |
| 2026-11-15 | [Backend Review Remediation Follow-ups](./20261115_170736_backend_review_remediation_followups.md) | Complete |
| 2026-11-22 | [Mobile-first UI and household URLs](./20261122_190603_mobile_first_ui.md) | Complete |
| 2026-11-24 | [Deterministic HTML template formatting](./20261124_083844_html_template_formatting.md) | Complete |
| 2026-11-24 | [iOS date input sizing](./20261124_220059_ios_date_input_sizing.md) | In progress |
| 2026-11-24 | [Household currency choices](./20261124_223027_household_currency_choices.md) | Proposed |
