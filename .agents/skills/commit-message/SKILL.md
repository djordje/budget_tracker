---
name: commit-message
description: Generate a consistently formatted commit message from staged changes without committing or requesting commit approval.
---

# Commit message

Use this skill whenever a commit message is needed, including when message generation is one step in a larger user-requested workflow.

## 1. Inspect staged changes

Review only the staged changes:

```sh
git status --short
git diff --cached --stat
git diff --cached --no-ext-diff
```

- If nothing is staged, tell the user and stop. Do not stage files automatically.
- Base the message exclusively on the staged diff, not on unstaged files, untracked files that are not staged, or prior conversation alone.
- Do not include or describe unstaged work in the proposed message.

## 2. Prepare the message

Use Conventional Commits with this format:

```text
<type>: <imperative summary>

<optional body explaining what and why>
```

### Subject

- Choose the primary type from the staged changes:
  - `feat`: user-facing behavior or a new capability
  - `fix`: incorrect behavior
  - `docs`: documentation only
  - `refactor`: internal restructuring without behavior changes
  - `test`: tests only
  - `build`: build system or dependencies
  - `ci`: continuous integration
  - `perf`: performance improvement
  - `style`: formatting only
  - `chore`: maintenance that does not fit another type
- Use a concise imperative summary such as `add spending entry endpoint`, not `added` or `adds`.
- Start the summary in lowercase unless it begins with a proper noun, and omit the trailing period.
- Prefer at most 50 characters and keep the complete subject within 72 characters.
- Do not add a scope unless it makes the change materially clearer and is used consistently in repository history.

### Body

- Add a body when it helps explain multiple related changes, important decisions, or motivation.
- Separate the body from the subject with one blank line and wrap lines at approximately 72 characters.
- Explain what changed and why; do not merely list filenames or repeat the diff.
- Do not add generated-by, AI attribution, or co-author trailers unless the user requests them.

Example:

```text
docs: add budget tracker MVP plan

Define the MVP scope, architecture, data model, money handling,
delivery milestones, and acceptance criteria. Add timestamped
planning-document conventions and an index for future sessions.
```

## 3. Return the message

Present the complete message in a fenced code block. The skill ends after the message is prepared; do not ask for approval and do not create a commit as part of this skill.

The user’s surrounding instruction determines what happens next:

- `Prepare commit message`: return the message only.
- `Prepare commit message and commit`: use the generated message for the separately requested commit.
- `Prepare commit message and ask me to validate it before committing`: return the message and follow the separately requested approval workflow.
- A plan may instruct the agent to use this skill and commit completed steps without adding an approval requirement.

Commit execution, approval, amendment, staging, and post-commit verification are outside this skill’s scope.
