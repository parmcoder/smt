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
updated: 2026-08-11
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
  Docker/OpenTofu components; immediately after the Web selection it asks
  `Include Flutter mobile application? [Y/n]`. Enter includes the Android/iOS
  Flutter Mobile component and an explicit no excludes it. It writes a
  validated `smt.yaml` blueprint only after confirmation and does not create a
  repository or workspace.
- `smt apply [--config FILE] PATH` — validate the supplied workspace
  blueprint/configuration, then create the root Git repository, selected local
  bootstrap submodules, ignore files, Beads metadata, and repository-local
  agent workflow files at a new destination. It does not install dependencies,
  create remote repositories, or call provider APIs.
- `smt push [--dry-run]` — preflight every configured repository, then push
  each child repository's current branch before the root. Remote URLs come from
  `repositories[].remote.url`; dry-run validates and prints the order without
  contacting remotes.
- `smt remote provision [--dry-run] [--json]` — discover or create every
  configured GitHub/GitLab project child-first, then persist returned SSH URLs
  and wire local origins only after all provider targets are available.
- `smt worktree add PATH --branch NAME [--dry-run]` — preflight the root and
  every configured submodule, then create one new linked worktree branch per
  repository at the same destination layout. It rejects dirty, detached,
  uninitialized, branch-colliding, gitlink-mismatched, or existing-destination
  state.
- `smt prepare` — after complete preflight, create the active Beads-ID branch
  in every configured repository. Tracked and untracked changes are stashed;
  ignored files are left in place. The operation has no positional arguments.
- `smt switch BEAD_ID` — switch every repository to an existing local branch
  named by the active Beads ID. It never creates branches, auto-pops stashes,
  or rolls back a partial switch.
- `smt pull` — fast-forward configured repositories child-first, then root,
  using each repository's effective default branch.
- `smt hooks install [--dry-run]` — require bare `smt` and `lefthook` on
  `PATH`, then preflight every configured initialized worktree, valid
  `lefthook.yml` `commit-msg` mapping, `lefthook validate` result, and eligible
  `commit-msg` hook before installing Lefthook dispatchers root-first. Dry-run
  prints the complete plan without mutation.
- `smt validate-message [--config FILE] FILE` — validate one complete
  conventional commit message. On a non-default active Beads branch it also
  requires the exact branch ID in `type(scope): [BEAD-ID] summary`; default
  branches use ordinary configured conventional-commit syntax.
- `smt status [--json]` — inspect configured repositories and summarize
  available profiles and contract findings.
- `smt doctor [--tree]` — report repository, Beads, hook, executable, remote,
  and profile readiness without printing secret values. The default is an
  action-first summary; `--tree` renders the detailed tree. `--verbose` adds
  safe diagnostic detail only.
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

## Flutter Mobile component

`smt new` asks the literal prompt `Include Flutter mobile application? [Y/n]`
immediately after the Web selection. Enter means Yes and an explicit no
excludes Mobile. When Mobile is selected, the component and repository order
is repo, web, mobile, api, database, infra; an opt-out omits Mobile.

Mobile extends, rather than changes, `version: 1`. Existing version-1
blueprints/configurations that lack Mobile remain valid; applying one produces
no Mobile output. When selected, `smt new` emits and `smt apply` accepts
exactly this stack entry and repository mapping:

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

Applying a selected Mobile blueprint creates the same Git-ready component
basics as the existing stacks: an independent initialized local bootstrap
submodule, a `mobile_worker` manifest, Flutter-oriented README and ignore
rules, and `.tool-versions` containing the literal pin `flutter 3.44.9`.
The Mobile component is strictly scaffold-only: SMT does not invoke `flutter
create`, `flutter --version`, or any other Flutter SDK CLI. It does not require
a Flutter executable or SDK, install dependencies, access the network, produce
Flutter application source, sign an app, or publish an app.

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
DevOps tools `docker` plus `opentofu`; the implemented Mobile extension adds
only `mobile: flutter` as described above. `ai_assist` is either absent or
`codex`.
`remote.url` is optional at initialization but required by `smt push`; it may
not contain embedded credentials. A repository
used by `smt remote provision` must declare `provider` and a fully qualified
`project` (`owner/repository` for GitHub, or `namespace/repository` for
GitLab). `visibility` accepts `private` or `public` and defaults to `private`.
Existing provider and project metadata remain optional for local-only
repositories.

Repositories may define `hook`, `submit`, and `ci-parity` profiles. A legacy
check list is accepted for compatibility, but new configuration should use
named profiles. Each check declares `kind`, non-empty `argv`, and, when
applicable, `mutates_worktree`; mutation is never inferred from the command.
Supported reusable contracts are literal `reference`, `migration-coverage`,
and `artifact` contracts. Paths must remain inside the workspace, IDs must be
unique, and contract severity is `error` unless explicitly set to `warn`.

Applied workspaces use local bootstrap URLs in `.gitmodules` because no remote
URL is required during blueprint application. `smt remote provision` replaces
those child entries with returned SSH clone URLs and updates each repository's
`origin` plus `remote.url` in one post-discovery wiring phase. It refuses
occupied or incompatible local remotes and never deletes provider projects.

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
- `checkout` and `validate-range` workflows from the earlier design.

Git lifecycle operations preflight all configured repositories before a remote
push or worktree creation. Pushes are child-first and stop after a failure with
the successful and pending repository IDs reported; no remote rollback occurs.
Worktree creation is root-first and stops with its created and pending paths
reported if a later child fails. Fixed untracked OS metadata (`.DS_Store`,
`Thumbs.db`, and `desktop.ini`) is ignored, while tracked changes remain
blocking. Provider projects and runtime tokens remain explicit
configuration/environment inputs.

## Workspace lifecycle, diagnostics, and hooks

The effective default branch is `repositories[].remote.default_branch` when
set, otherwise `main`. `prepare` and `switch` use Beads readiness from the root
workspace: the active Beads ID is the branch name, and hooks require the
workspace to be ready before accepting a commit. Commit subjects on an active
Beads branch use `type(scope): [BEAD-ID] summary`; outside one, use the normal
configured conventional-commit syntax. The root may use the active Beads ID;
there are no Jira aliases, assignment waves, manifests, or provider review
automation in this release.

`status` is a human report: it starts with `STATUS: OK`, `WARN`, or `ERROR`,
then shows each configured repository's path, Git state, branch, and
`commit-msg` hook state, followed by configured profile names, contract counts,
and safe next steps. `status --json` is the machine-readable alternative; it
returns repository entries plus profile names and contract error/warning
counts. When no profiles are configured, the human report writes
`profiles: none`; JSON remains `profiles: []`. Diagnostics are state-oriented
and do not print token values, command arguments, or command output.

Hook state is local to each configured repository: `absent` means no
`commit-msg` hook is present; `current` means the hook exactly matches a
recognized historical SMT script or the reviewed Lefthook 2.1.10 dispatcher;
`unmanaged` means another, lookalike, modified, symlinked, directory, or other
nonregular hook target exists. SMT neither follows nor overwrites unmanaged
hooks. Resolve those targets manually before running `smt hooks install`; no
`--force` or chaining mode exists. An exact recognized historical SMT hook is
eligible for migration only when no `commit-msg.old` entry exists. Lefthook
2.1.10 may preserve it as `commit-msg.old` during dispatcher installation. A
current Lefthook dispatcher with an existing `.old` entry remains allowed.

`doctor` is also read-only. It always checks Git, bare `smt`, and Lefthook, and
renders a deterministic repository-first tree. Each configured repository is
expanded in configuration order with `worktree`, `hook`, `remote`, and
`provider` children; `tools` and `credentials` are separate roots. A missing
`smt` or Lefthook is an error and produces an error exit; its remediation
directs the user to return to the SMT source checkout, build the CLI, and
expose `$PWD/bin` on `PATH`, or install Lefthook and rerun `smt doctor`, before
attempting hook installation. An absent hook or unset remote is a warning.
Provider absence is an acceptable local-only state, and configured provider
projects are summarized without URLs, tokens, command output, or private
inspection errors. Missing provider tokens are warnings because Git pushes and
manual PR/MR handoff remain available. Explanations and exact remediation are
printed below the affected warning/error node; no global duplicate next-step
list is emitted. `READY`, `WARN`, and `ERROR` remain the overall exit classes.

`smt hooks install` plans only after every configured repository passes its
preflight, so a failing child prevents all installation. It resolves `smt` and
`lefthook` with `exec.LookPath`. Before any installer mutation, it runs
argument-array `git config --get core.hooksPath` in every initialized configured
repository; any nonempty effective setting, including a relative path, blocks
the entire plan as a manually resolved custom hook-path policy. It then checks
each repository is an initialized Git worktree with a top-level `commit-msg`
mapping in `lefthook.yml`, and treats symlink, directory, and other nonregular
`commit-msg` targets as unmanaged blockers. It uses argument-array execution
for `lefthook validate` in every repository and, only after successful
preflight, for root-first `lefthook install commit-msg`.

Unmanaged custom, lookalike, modified, and nonregular hooks are never followed
or overwritten. For every exact legacy SMT `commit-msg` hook, SMT also
preflights `commit-msg.old`: if any entry exists, including a symlink, both
`smt hooks install` and `--dry-run` reject the entire plan before root-first
execution. Lefthook 2.1.10 itself refuses that migration without `--force`; SMT
requires manual collision resolution instead. The exact legacy hook is eligible
only when `.old` is absent, at which point Lefthook may preserve it as
`commit-msg.old` while installing the dispatcher. A current Lefthook dispatcher
with an existing `.old` entry remains allowed. Collision errors do not disclose
paths or hook contents. This does not authorize `--force`, shell execution,
resetting a collision, overwriting unmanaged hooks, or rollback of an earlier
install. Successful installation prints the installed repository IDs. If an
unexpected later install fails, SMT reports installed and pending IDs for manual
recovery.

Generated workspaces include root and child `lefthook.yml` files with
top-level `no_auto_install: true` and `assert_lefthook_installed: true`, plus a
`commit-msg` command that invokes bare
`smt validate-message --config FILE {1}` using the correct relative path to
the root configuration. `no_auto_install` prevents Lefthook from automatically
installing or updating hooks when configuration changes. The Lefthook assertion
makes Git fail the hook if Lefthook cannot be found, preventing a silent skip
of `smt validate-message`. In the SMT source checkout, `task build` creates
`bin/smt` but does not put it on `PATH`; use `export PATH="$PWD/bin:$PATH"`
there, then return to the target workspace for `smt doctor` or hook
installation. Both `smt` and Lefthook must remain durably available on PATH for
every hook-running environment, including IDE or GUI launches. Workspace
creation writes that scaffold only; it does not run Lefthook or install a hook.

Fixture evidence is bounded rather than universal: a clean fixture installed
the dispatcher in every configured repository and accepted a normal commit.
After deliberately removing the installer-provided Lefthook binary while
leaving `smt` on PATH, Git rejected an otherwise valid commit with Lefthook's
assertion error. This demonstrates the no-silent-skip boundary, not a completed
human end-to-end review across every launch environment.

<!-- INACTIVE: Prepared feature workspaces use `.smt/runs/<feature-id>.json` as an ignored,
secret-free snapshot of the feature, base branches/commits, repository paths,
ownership boundaries, check-profile names, integration gates, and assigned
work-item context. The file is written only after the root and child linked
worktrees are created successfully. A prepared child accepts only its assigned
Beads IDs or Jira-shaped aliases. The root accepts the feature ID and its
assigned root-task references for integration/gitlink commits. Missing,
malformed, cross-repository, ambiguous, corrupt, or out-of-date manifests fail
closed; outside a prepared workspace the existing conventional-commit behavior
is unchanged. Dry-run preparation performs no Git, Beads, or manifest write. -->

<!-- INACTIVE historical provider submission design.
## Provider remotes and workspace submission

Provider tokens are environment-only: `SMT_GITHUB_TOKEN` and
`SMT_GITLAB_TOKEN`. Provider API bases use the configured GitHub Enterprise or
self-managed GitLab endpoint, with public GitHub/GitLab defaults otherwise.
Provisioning checks all tokens, initialized clean repositories, exact project
identities, and local origin conflicts before creating anything. Missing empty
projects are created child-first; compatible existing projects are reused.
Provider-created state is never deleted as rollback. A provider partial failure
reports created, existing, and pending IDs for a later idempotent rerun.

Submission reads only the prepared manifest. It requires clean attached
worktrees, configured matching origins, available recorded target branches,
valid assigned commit references, and passing `submit` profiles before the
first push. Changed children require a root gitlink commit. Children push before
the root without force or history rewriting. Existing open PRs/MRs with the
same source and target are reused; otherwise drafts are created, with
`--ready` promoting or creating ready reviews.

When a provider token is missing, Git pushes still complete and SMT prints a
provider creation link plus copy-ready title and body. Root reviews are
deferred until selected child review URLs exist. Provider errors report
completed and pending progress without remote rollback. Real GitHub/GitLab
provisioning and mixed-provider review acceptance remain the human-owned
`smt-5w0.13.6` release gate.

## Traceability and provider release gates

The agent-owned implementation is covered by focused tests, full Go gates,
and the command recipes. Final acceptance still requires two human reviews in
disposable workspaces:

- `smt-5w0.11.7` must verify the prepared manifest's base branch, repository
  ownership, assigned Beads/Jira references, dry-run immutability, rejection
  of missing/arbitrary/cross-repository IDs, valid child commits, and the root
  feature integration commit. Corrupt, missing, ambiguous, or stale manifest
  state must fail closed. Agents must not close this ticket.
- `smt-5w0.13.6` must verify real mixed-provider project creation or exact
  reuse, SSH wiring, child-first/root-last submission, missing-token handoff,
  draft and ready review behavior, idempotent review reuse, `Closes` lines,
  child links in the root review, and secret-safe failures. Agents must not
  close this ticket.

The exact commands and evidence expectations are in the
[[../10-development/SMT - Command Recipes#Traceable workspace release-gate handoff]]
section. Record failures as linked Beads bugs and leave the corresponding
review and parent feature open.

## Local requirements and verification -->

## Local requirements and verification

Go 1.26.4 or newer and `git` are required. The SMT source checkout's
`Taskfile.yml` is the repeatable entrypoint: `task build` creates `bin/smt` and
`task verify` runs the Go tests. The implementation must keep focused tests for
configuration, scaffolded Git submodules, push/worktree preflight and recovery
reporting, hook preflight/installation, custom hook-path, nonregular-hook, and
migration/collision behavior, harmless metadata, profiles and mutation guards,
status/doctor output, contract severity and path validation, CI audit, and
guarded bump planning/apply behavior.

The Mobile focused-test contract covers default inclusion, explicit
opt-out, invalid-answer retry, EOF/decline no-write, exact YAML/repository
mapping/scopes/order, invalid stack or metadata rejection before mutation,
existing-version-1 compatibility, atomic cleanup for preflight and
stage/publish failures, generated artifacts, and focused tests without Flutter.
Human end-to-end confirmation is later human-owned work (`smt-3r2.5`), not
completed runtime proof in this delivery.

## Flutter Mobile delivery order

The contract, blueprint, atomic apply, and documentation/release verification
work are complete through `smt-3r2.4`. The human-owned E2E review remains
`smt-3r2.5`.

## Human E2E review handoff

`smt-3r2.5` should run `smt new` twice in clean temporary locations: first
press Enter at the Mobile prompt and confirm `mobile: flutter`, the Mobile
repository entry, and the Mobile-selected `repo`, `web`, `mobile`, `api`,
`database`, `infra` order; then explicitly answer no and confirm no Mobile
configuration. Apply
each reviewed blueprint to a new destination. For the default case, inspect
the Git-ready `mobile-app` submodule, `agents/mobile_worker.toml`, Mobile
README and ignore rules, and `.tool-versions` Flutter `3.44.9` pin. Do not
expect or attempt Flutter source generation, SDK/CLI use, dependency install,
network access, signing, or store publication; record the observed commands
and artifacts as human-review evidence. At one additional fresh destination,
exercise one safe prerequisite, staging, Beads, or publish failure and verify
that no partial destination remains.

## Related

- [[SMT - Product Concept]] — compact product framing.
- [[SMT - Agent Team]] — ownership and documentation workflow.
- [[../../prompts/smt-build|SMT build prompt]] — execution handoff.
- [[../../AGENTS|Repository operating agreement]].
