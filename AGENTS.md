# SMT Agent Operating Agreement

## Mission

Build `smt` (Sanovy Mono Tool), a small Go CLI that coordinates safe local
work across a Git root repository and its independent submodules. The
implementation specification in `docs/00-project/SMT - Implementation Spec.md`
is the source of truth for behavior and acceptance criteria.

## Working agreements

- Read the relevant note in `docs/` before planning or editing code.
- The next release uses Bubble Tea for the interactive TTY workflow and
  go-git for every compiled SMT Git operation. Keep explicit headless
  subcommands deterministic; never shell out to a system `git` executable,
  execute shell-string commands, or create a credential store.
- Never log or persist `SMT_GITLAB_TOKEN`, authorization headers, credentials,
  or sensitive payloads.
- Preserve existing user changes and inspect root/submodule status before Git
  operations.
- Preserve complete push preflight and child-before-root ordering. Never
  rewrite history or run destructive remote rollback after a partial push
  failure. Linked-worktree creation is removed from the next-release scope.
- Add focused tests for each new behavior and report exact verification
  commands, assumptions, risks, and unverified behavior.
- Keep documentation Obsidian-friendly: frontmatter, stable titles, wikilinks,
  short summaries, and Mermaid diagrams when a flow is clearer visually.

## Agent routing

The active delivery path uses a Terra work manager, a Luna implementation
worker, and a Luna documentation worker. `backend_agent` remains available for
an explicitly requested standalone architecture review, but it is not a child
of `work_manager` and must not share an active implementation scope with it:

Their manifests live under `agents/` because the host-managed `.codex/` and
`.agents/` directories are not writable in this checkout.

- `work_manager`: Terra/high delivery controller. It coordinates exactly
  `backend_worker` and `doc_writer`, serializes backend assignments, performs
  the final review loop, and never implements Go code. Use
  `$godex:godex-go-backend` for Go architecture and review only.
- `backend_worker`: Luna/medium worker for assigned Go implementation and
  tests. It accepts assignments only from `work_manager` and is the sole owner
  of manager-assigned Go production code and focused Go tests under `internal/`
  and `cmd/smt/`; it never delegates further. Use `$godex:godex-go-backend`.
- `doc_writer`: docs, prompts, handoffs, and diagrams under `docs/` and
  `prompts/`, using Luna/low. Use `$codex-obsidian-writer` and
  `$codex-obsidian-markdown`. It remains a high-level documentation worker and
  does not implement Go behavior.
- `backend_agent`: Terra/high Go architecture and review controller for direct,
  non-work-manager requests. It does not issue `backend_worker` implementation
  assignments. Use `$godex:godex-go-backend`.

For work managed by `work_manager`, the required loop is
`work_manager -> backend_worker -> work_manager`: the manager issues one
decision-complete Go assignment at a time, the worker implements and runs its
task-level tests, and the manager reviews the integrated diff. Blocking
findings return only to the same worker. The manager never writes Go; it
serializes integration and owns final acceptance. `doc_writer` is coordinated
after accepted behavior to keep durable documentation and prompts aligned, but
does not alter Go behavior. No agent in this loop delegates further.

Every accepted feature then queues a human-owned E2E review in Beads. The
feature remains open until a human records pass evidence and closes the review.
A failed review must create and block on a child bug; closing that bug requeues
the same human review for retest. Related ready work may continue, but release
readiness is blocked by any open human review or related bug. Agents must never
approve or close human-owned reviews.

Every worker handoff and manager review must list changed paths, checks and
results, assumptions, unresolved risks, and unverified behavior. A worker may
not begin an ambiguous assignment; it must return the precise missing decision
and implementation/test evidence to `work_manager`.

## Context7 documentation rule

When a task asks about a library, framework, SDK, API, CLI tool, or cloud
service, use Context7 before answering or implementing library-specific
behavior:

```sh
npx ctx7@latest library "<official library name>" "<full question>"
npx ctx7@latest docs "<resolved /org/project id>" "<full question>"
```

Use no more than three Context7 commands for one question, do not include
secrets in queries, and run the command outside the default sandbox when
network access requires it. This rule does not apply to ordinary refactoring,
new scripts, business-logic debugging, or general programming concepts.

## Completion report

Every agent handoff must list changed paths, checks run and results,
assumptions, unresolved risks, and unverified behavior. The parent task owns
the final integration check.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:970c3bf2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

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
   bd dolt push
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->

<!-- BEGIN BEADS CODEX SETUP: generated by bd setup codex -->
## Beads Issue Tracker

Use Beads (`bd`) for durable task tracking in repositories that include it. Use the `beads` skill at `.agents/skills/beads/SKILL.md` (project install) or `~/.agents/skills/beads/SKILL.md` (global install) for Beads workflow guidance, then use the `bd` CLI for issue operations.

### Quick Reference

```bash
bd ready                # Find available work
bd show <id>            # View issue details
bd update <id> --claim  # Claim work
bd close <id>           # Complete work
bd prime                # Refresh Beads context
```

### Rules

- Use `bd` for all task tracking; do not create markdown TODO lists.
- Run `bd prime` when Beads context is missing or stale. Codex 0.129.0+ can load Beads context automatically through native hooks; use `/hooks` to inspect or toggle them.
- Keep persistent project memory in Beads via `bd remember`; do not create ad hoc memory files.

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.
<!-- END BEADS CODEX SETUP -->
