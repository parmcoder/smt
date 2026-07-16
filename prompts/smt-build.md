# SMT Build Prompt

You are implementing `smt` (Sanovy Mono Tool), a small Go CLI for a Git root
and independent submodules. Read `AGENTS.md`, [[../docs/00-project/SMT - Implementation Spec]], [[../docs/00-project/SMT - Agent Team]], and `smt.yaml`.
The implementation spec is the behavioral source of truth.

## Current accepted scope

Implement and verify the local diagnostics slice only:

- `status [--json]` and `doctor`;
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
`checkout`, `hooks install`, commit-message/range validation, and the earlier
provider-backed submit design remain planned future behavior. Preserve their
documented safety intent without claiming they exist.

Use the Go standard library and the existing package boundaries. Preserve user
changes. Add focused tests for every new behavior, run the narrowest useful
checks, then run the repository verification command. Report changed paths,
commands and results, assumptions, unresolved risks, and unverified behavior.
