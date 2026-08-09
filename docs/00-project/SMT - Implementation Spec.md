---
type: implementation-spec
status: active
owner: platform
tags:
  - go
  - git
  - gitlab
  - github
  - monorepo
  - developer-experience
created: 2026-07-15
updated: 2026-08-09
---
# SMT — Sanovy Mono Tool

## Summary

`smt` is a small Go CLI for a Git root plus independent submodules.
Configuration is committed in `smt.yaml`, remains at `version: 1`, and contains
no credentials. The accepted implementation includes local workspace
scaffolding, guarded Git lifecycle operations, diagnostics, checks, and
contract inspection.

The Cobra root help groups commands as Getting Started, Workspace, Review
Workflow, and Developer Tools. Implemented commands are:

- `smt new [FILE]` — interactively select fixed Next.js, Go, PostgreSQL, and
  Docker/OpenTofu components and write a validated `smt.yaml` blueprint only
  after confirmation. It does not create a repository or workspace.
- `smt apply [--config FILE] PATH` — validate the supplied workspace
  blueprint/configuration, then create the root Git repository, selected local
  bootstrap submodules, ignore files, Beads metadata, and repository-local
  agent workflow files at a new destination. It does not install dependencies,
  create remote repositories, or call provider APIs.
- `smt push [--dry-run]` — preflight every configured repository, then push
  each child repository's current branch before the root. Remote URLs come from
  `repositories[].remote.url`; dry-run validates and prints the order without
  contacting remotes.
- `smt worktree add PATH --branch NAME [--dry-run]` — preflight the root and
  every configured submodule, then create one new linked worktree branch per
  repository at the same destination layout. It rejects dirty, detached,
  uninitialized, branch-colliding, gitlink-mismatched, or existing-destination
  state.
- `smt validate-message FILE` — validate one complete conventional commit
  message against the configured types and scopes.
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
- `smt work ready [--json]`, `smt review`, `smt review list [--json]`,
  `smt review queue FEATURE --handoff PATH --evidence PATH [--json]`, and
  `smt review requeue REVIEW [--json]` — expose the retained local
  work/review workflow.
- `smt release check [--json]` — report release readiness.
- `smt completion bash|fish|powershell|zsh` — generate static shell
  completion; this command and `smt --help` do not require `smt.yaml`.

These commands use argument arrays, never force-push or rewrite history, and
never log or persist tokens, authorization headers, or sensitive payloads.

## Approved Flutter Mobile component (planned)

This is the approved contract for the next Mobile delivery; it does **not**
claim that the current Go implementation already supports Mobile. `smt new`
will ask the literal prompt `Include Flutter mobile application? [Y/n]`
immediately after the Web selection. Enter means Yes and an explicit no
excludes Mobile. The selected component and repository order is always: repo,
web, mobile, api, database, infra.

Mobile extends, rather than changes, `version: 1`. Existing version-1
blueprints/configurations that lack Mobile remain valid; applying one produces
no Mobile output. When selected, `smt new` must emit and `smt apply` must
accept exactly this stack entry and repository mapping:

```yaml
workspace:
  stack:
    mobile: flutter

repositories:
  - id: mobile
    path: mobile-app
    component: mobile
    technology: flutter
    scope: mobile
```

Only `flutter` is supported for the Mobile stack in version 1. Any unsupported
Mobile stack or mismatched Mobile metadata is a validation failure before any
destination mutation. Version 1 targets Android and iOS only; store signing,
metadata, submission, and publication are outside this workspace-setup scope.

Applying a selected Mobile blueprint will create the same Git-ready component
basics as the existing stacks: an independent initialized local bootstrap
submodule, a `mobile_worker` manifest, Flutter-oriented README and ignore
rules, and `.tool-versions` containing the literal pin `flutter 3.44.9`.
The Mobile component is strictly scaffold-only: SMT must not invoke `flutter
create`, `flutter --version`, or any other Flutter SDK CLI; require a Flutter
executable or SDK; install dependencies; access the network; or promise
generated Flutter application source.

`smt apply` validates first and remains atomic/all-or-nothing. Any
prerequisite, staging, Beads, or publish failure leaves no partial destination.
Existing all-or-nothing semantics remain, and SMT must never attempt remote
rollback after a later submit failure.

## Configuration contract

The root `smt.yaml` uses this shape (the existing file is canonical):

```yaml
version: 1
workspace:
  ai_assist: codex
  stack:
    web: nextjs
    api: go
    database: postgresql
    devops: [docker, opentofu]

repositories:
  - id: repo
    path: .
    scope: repo
    remote:
      url: ""
  - id: web
    path: web-app
    component: web
    technology: nextjs
    scope: web
    remote:
      url: git@github.com:example/web-app.git

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

The fixed workspace stack values are `nextjs`, `go`, `postgresql`, and the
DevOps tools `docker` plus `opentofu`; the approved planned Mobile extension
adds only `mobile: flutter` as described above. `ai_assist` is either absent or
`codex`.
`remote.url` is optional at initialization but required by `smt push`; it may
not contain embedded credentials. Existing `provider` and `project` metadata
remain supported and are optional when both are absent.

Repositories may define `hook`, `submit`, and `ci-parity` profiles. A legacy
check list is accepted for compatibility, but new configuration should use
named profiles. Each check declares `kind`, non-empty `argv`, and, when
applicable, `mutates_worktree`; mutation is never inferred from the command.
Supported reusable contracts are literal `reference`, `migration-coverage`,
and `artifact` contracts. Paths must remain inside the workspace, IDs must be
unique, and contract severity is `error` unless explicitly set to `warn`.

Applied workspaces use local bootstrap URLs in `.gitmodules` because no remote
URL is required during blueprint application. `push` uses `remote.url` directly
and does not rewrite `.gitmodules`; replacing bootstrap URLs for fresh external
clones is a separate future capability.

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
- `checkout`, remote-URL synchronization into `.gitmodules`, `hooks install`,
  `validate-range`, and `submit` workflows from the earlier design.

Git lifecycle operations preflight all configured repositories before a remote
push or worktree creation. Pushes are child-first and stop after a failure with
the successful and pending repository IDs reported; no remote rollback occurs.
Worktree creation is root-first and stops with its created and pending paths
reported if a later child fails. Fixed untracked OS metadata (`.DS_Store`,
`Thumbs.db`, and `desktop.ini`) is ignored, while tracked changes remain
blocking. Provider projects and runtime tokens remain explicit
configuration/environment inputs.

## Local requirements and verification

Go 1.26.4 or newer and `git` are required. The root `Taskfile.yml` is the
repeatable entrypoint: `task build` creates `bin/smt` and `task verify` runs
the Go tests. The implementation must keep focused tests for configuration,
scaffolded Git submodules, push/worktree preflight and recovery reporting,
harmless metadata, profiles and mutation guards, status/doctor output, contract
severity and path validation, CI audit, and guarded bump planning/apply behavior.

The planned Mobile focused-test contract covers default inclusion, explicit
opt-out, invalid-answer retry, EOF/decline no-write, exact YAML/repository
mapping/scopes/order, invalid stack or metadata rejection before mutation,
existing-version-1 compatibility, atomic cleanup for preflight and
stage/publish failures, generated artifacts, and focused tests without Flutter.
Human end-to-end confirmation is later human-owned work (`smt-3r2.5`), not
completed runtime proof in this delivery.

## Flutter Mobile delivery order

`smt-3r2.1` defines this contract and blocks configuration/blueprint work in
`smt-3r2.2`; that then precedes atomic apply work in `smt-3r2.3`, final
documentation/release verification in `smt-3r2.4`, and the human-owned E2E
review in `smt-3r2.5`.

## Related

- [[SMT - Product Concept]] — compact product framing.
- [[SMT - Agent Team]] — ownership and documentation workflow.
- [[../../prompts/smt-build|SMT build prompt]] — execution handoff.
- [[../../AGENTS|Repository operating agreement]].
