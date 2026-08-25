# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

## Human-in-the-loop delivery loop

Use this loop for repository work that changes code, tests, documentation, or
Beads state. The human review checkpoint is mandatory; a review handoff is not
permission to commit, push, close a task, or create a pull request.

### Orient and scope

- Run `bd prime`, inspect the relevant issue with `bd show`, and inspect the
  current branch and worktree before editing.
- Work one bounded Beads task at a time. Update its description, design, or
  acceptance criteria when implementation decisions change.
- Stop and return the precise missing decision when the assignment is
  ambiguous; do not widen scope by assumption.
- Preserve unrelated user changes and never stage, revert, or rewrite them.

### Implement and align

- Keep implementation, focused tests, relevant documentation, and Beads
  acceptance criteria synchronized.
- Report unavailable toolchains, device lanes, runtime dependencies, and
  other unverified behavior explicitly.
- When the repository commit hook is active, use the exact active Beads ID as
  the non-default branch name. The commit subject must be
  `type(scope): [BEAD-ID] summary`, with the bracketed ID matching the branch
  exactly. Keep the task `open` or `in_progress` through the implementation
  commit.

### Human review checkpoint

Before delivery, stop and provide a review handoff containing:

- changed paths;
- checks and results;
- assumptions;
- unresolved risks;
- unverified behavior; and
- a proposed conventional commit or pull-request title and description.

Do not commit, push, close the task, or create a pull request until the user
explicitly authorizes that action. “Review” or “looks good” is not permission
to perform a broader delivery action unless the authorization is clear.

### Approved delivery

- Re-run the final checks after approval.
- Use ordinary Conventional Commit syntax on `main`; use the exact Beads
  reference format above on an active task branch.
- Push only the explicitly authorized target branch. Direct pushes to `main`
  require explicit user authorization.
- After pushing, report the commit hash, remote branch, clean-worktree state,
  and local/remote hash match.
- Provide a copy-ready conventional pull-request title, Markdown description,
  and pull-request creation link. Never claim that a pull request was opened
  unless it was actually created.

### Beads closure

- Close completed work only after its implementation commit is recorded.
- Run `bd lint` and `bd orphans` before the final handoff.
- If a tracker-only closure commit is rejected because the repository hook
  requires the active task to remain open, stop and obtain explicit approval
  before using any hook bypass. Never use `--no-verify` silently.
- Keep parent tasks open unless their own acceptance criteria are complete.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:6cd5cc61 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd create             # Create a feature or task ticket directly
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd blocked            # Find blocked work
bd close <id>         # Complete work
```

Agents create and manage feature or task tickets directly with Beads; SMT no
longer wraps ticket creation, review queues, ready-work listing, or release
readiness. Create the implementation ticket before editing code. `smt prepare`
may still create its special internal `Prepared workspace` task.

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->


## Build & Test

_Add your build and test commands here_

```bash
# Example:
# npm install
# npm test
```

## Architecture Overview

_Add a brief overview of your project architecture_

## Conventions & Patterns

_Add your project-specific conventions here_
