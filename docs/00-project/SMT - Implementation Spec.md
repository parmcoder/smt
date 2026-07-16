---
type: implementation-spec
status: proposed
owner: platform
tags:
  - go
  - git
  - gitlab
  - github
  - monorepo
  - developer-experience
created: 2026-07-15
updated: 2026-07-16
---
# SMT — Sanovy Mono Tool

## Summary

`smt` is a small, standard-library Go CLI for a Git root plus independent
submodules. Configuration is committed in `smt.yaml`, remains at `version: 1`,
and contains no credentials. The accepted first implementation is local,
read-only diagnostics plus guarded configured checks and contract inspection.

Implemented commands are:

- `smt status [--json]` — inspect configured repositories and summarize
  available profiles and contract findings.
- `smt doctor` — report repository, executable, provider-token, and profile
  readiness without printing secret values.
- `smt check --profile hook|submit|ci-parity [--repo ID]
  [--allow-worktree-mutation] [--dry-run]` — run one named profile. A check
  with `mutates_worktree: true` is refused unless the explicit
  `--allow-worktree-mutation` flag is supplied.
- `smt contracts validate` — evaluate reusable configured contracts and report
  all findings. Severity defaults to `error`; `severity: warn` is explicit.
- `smt ci audit` — run the CI-parity contract/profile audit and return a
  validation failure when error-severity findings exist.
- `smt ci contracts bump --id ID [--apply]` — plan a reference-literal bump by
  default; write only with explicit `--apply`. Stale, absent, or ambiguous
  literals are guarded failures.

These commands use argument arrays, preserve worktree state unless explicitly
allowed, and never log or persist tokens, authorization headers, or sensitive
payloads.

## Configuration contract

The root `smt.yaml` uses this shape (the existing file is canonical):

```yaml
version: 1
repositories:
  - id: database
    path: database
    provider: gitlab
    project: sanovy/database
    scope: database
    checks:
      hook:
        - kind: sql-format
          argv: [pg_format]
          include: ["**/*.sql"]
          mutates_worktree: true
      submit: []
      ci-parity: []

contracts:
  reference:
    - id: ci-pin
      repository: repo
      file: .gitlab-ci.yml
      expected: old
      replacement: new
      severity: warn
  migration-coverage:
    - id: migration
      repository: database
      file: migrations/001.sql
      source: docs/migration.md
      expected: delivered
  artifact:
    - id: bundle
      repository: web
      file: dist/app.js
      expected: present
```

Repositories may define `hook`, `submit`, and `ci-parity` profiles. A legacy
check list is accepted for compatibility, but new configuration should use
named profiles. Each check declares `kind`, non-empty `argv`, and, when
applicable, `mutates_worktree`; mutation is never inferred from the command.
Supported reusable contracts are literal `reference`, `migration-coverage`,
and `artifact` contracts. Paths must remain inside the workspace, IDs must be
unique, and contract severity is `error` unless explicitly set to `warn`.

Reference bumps replace exactly one current literal. The default is a plan;
`--apply` is required to write, and the command refuses stale, missing, or
ambiguous matches.

## Explicitly planned, not implemented

The following remain approved future behavior and must not be represented as
implemented by the CLI or this release:

- changesets, release plan, and release run;
- cloud or database actions, deployment, rollback, or provider-native job
  execution;
- YAML selector rewrites or automatic CI configuration edits;
- GitLab/GitHub provider calls, MR/PR creation, credential use, and mixed
  provider submit orchestration;
- `checkout`, `hooks install`, `validate-message`, `validate-range`, and
  `submit` workflows from the earlier design.

The planned Git workflows retain these safety requirements: preflight all
repositories before checkout or commit, commit submodules before the root
gitlink, use Git argument arrays, never rewrite history or remotely roll back,
and report exact recovery actions after partial remote failure. Provider
projects and runtime tokens remain explicit configuration/environment inputs.

## Local requirements and verification

Go 1.26.4 or newer and `git` are required. The root `Taskfile.yml` is the
repeatable entrypoint: `task build` creates `bin/smt` and `task verify` runs
the Go tests. The implementation must keep focused tests for configuration,
profiles and mutation guards, status/doctor output, contract severity and
path validation, CI audit, and guarded bump planning/apply behavior.

## Related

- [[SMT - Product Concept]] — compact product framing.
- [[SMT - Agent Team]] — ownership and documentation workflow.
- [[../../prompts/smt-build|SMT build prompt]] — execution handoff.
- [[../../AGENTS|Repository operating agreement]].
