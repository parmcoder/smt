# SMT Build Prompt

You are implementing `smt` (Sanovy Mono Tool), a small Go CLI for a Git root
and independent submodules. Read `AGENTS.md`, [[../docs/00-project/SMT - Implementation Spec]], [[../docs/00-project/SMT - Agent Team]], and `smt.yaml`.
The implementation spec is the behavioral source of truth.

## Current accepted scope

The active lifecycle is `prepare`, `switch`, and `pull`: `prepare` takes no
positional arguments, preflights all repositories, creates the active Beads-ID
branch, stashes tracked and untracked changes, and leaves ignored files. `switch
BEAD_ID` selects only an existing local branch with no auto-pop or rollback.
`pull` fast-forwards children before root using repository
`remote.default_branch`, then `main`.
Hooks require Beads readiness. Active-branch commits use
`type(scope): [BEAD-ID] summary`; ordinary branches use normal conventional
syntax. Workspace prepare/submit manifests, Jira aliases, assignment waves,
and provider review automation are out of scope and removed.

Implement and verify the local workspace diagnostics and hook slice:

- `status [--json]` and `doctor`;
- human status uses `profiles: none` with no configured profiles, while
  `status --json` retains `profiles: []`;
- `hooks install [--dry-run]` with all-repository preflight, per-repository
  argument-array `git config --get core.hooksPath` and `lefthook validate`,
  plus root-first `lefthook install commit-msg` installation;
- generated root and child `lefthook.yml` with top-level
  `no_auto_install: true` and `assert_lefthook_installed: true`, delegating to
  bare `smt validate-message --config FILE {1}`;
- `validate-message [--config FILE] FILE`;
- `check --profile hook|submit|ci-parity`, with optional `--repo`,
  `--dry-run`, and explicit `--allow-worktree-mutation`;
- `contracts validate` and `ci audit`;
- `ci contracts bump --id ID [--apply]`, plan-only by default, with stale,
  absent, and ambiguous literal guards.

Keep `smt.yaml` at configuration version 1. Support reusable literal
`reference`, `migration-coverage`, and `artifact` contracts. Contract severity
defaults to `error`; `warn` must be explicit. Use argument arrays, contain all
paths within the workspace, and never print or persist secrets.

## Explicit non-scope

Do not implement changesets, release plan/run, cloud or database actions,
provider calls, YAML selector rewrites, or GitLab/GitHub MR/PR submission.
`checkout`, `validate-range`, and the earlier provider-backed submit design
remain planned future behavior. Preserve their documented safety intent without
claiming they exist. Hook installation must never force, overwrite an unmanaged
custom, lookalike, modified, symlinked, directory, or other nonregular
`commit-msg` target, or roll back a partial root-first install. Any nonempty
effective `core.hooksPath`, including a relative path, is a manually resolved
custom hook-path policy that blocks the complete plan. An exact recognized
historical SMT hook is eligible for migration, and Lefthook 2.1.10 may preserve
it as `commit-msg.old` only when no `.old` entry exists. If `commit-msg.old`
exists, including as a symlink, both the real install and `--dry-run` must
reject the whole plan before root-first execution; a current Lefthook dispatcher
with `.old` remains allowed. Lefthook would require `--force` for that legacy
migration, but SMT must require manual collision resolution without exposing
paths or hook contents. This never authorizes force, reset, shell execution, or
overwriting unmanaged hooks. It requires bare `smt` and Lefthook on `PATH`.
From the SMT source checkout, the supported setup is `task build`, then
`export PATH="$PWD/bin:$PATH"`, followed by a return to the target workspace
for `smt doctor` and hook installation. `doctor` must check Git, smt, and
Lefthook and guide missing-tool remediation before hook installation. Resolve
both tool names with `exec.LookPath`; use argument-array execution for Git
config plus `lefthook validate` and `lefthook install commit-msg`. The generated assertion
must make a missing Lefthook binary fail the Git hook instead of silently
skipping validation. Both tools need durable PATH availability in shell, IDE,
and GUI hook-launch environments. `apply` may generate this bare-`smt`
Lefthook configuration but must not execute Lefthook.

Use the Go standard library and the existing package boundaries. Preserve user
changes. Add focused tests for every new behavior, run the narrowest useful
checks, then run the repository verification command. Report changed paths,
commands and results, assumptions, unresolved risks, and unverified behavior.

## Active branch commit contract

Default branches use ordinary conventional-commit syntax. On a non-default
active Beads branch, require the exact branch ID immediately after the prefix:

```text
type(scope): [BEAD-ID] summary
```

There is no manifest exception for root integration commits. Workspace
prepare/submit manifests, Jira aliases, assignment waves, and provider review
automation are removed from the active release and must not be implemented.

<!-- Historical prepared-workspace contract retained below for provenance only.

When a workspace was prepared for a feature, every commit subject must use:

```text
type(scope): [WORK-ID] summary
```

The bracketed ID must be immediately after the conventional prefix and must be
assigned to the current repository in the prepared run manifest. A child may
use its assigned Beads ID or Jira-shaped alias; the root may additionally use
the feature ID and assigned root-task IDs for integration/gitlink commits.
Outside a prepared workspace, retain the normal configured conventional-commit
validation. -->
