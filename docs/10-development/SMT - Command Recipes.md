---
type: command-recipes
status: active
owner: platform
tags:
  - smt
  - cli
  - development
  - release
created: 2026-07-16
updated: 2026-08-11
---
# SMT — Command Recipes

These examples assume Go plus Task are installed. Build first in the SMT source
checkout when using `bin/smt`; commands that inspect or operate an existing
workspace require a valid `smt.yaml`.

## Create a platform workspace

From the SMT repository root, create the blueprint outside this checkout:

```sh
mkdir -p ../platform-config
bin/smt new ../platform-config/smt.yaml
```

`new` interactively selects the fixed Next.js, Go, PostgreSQL, and
Docker/OpenTofu components. Immediately after Web, it asks `Include Flutter
mobile application? [Y/n]`: Enter includes the Android/iOS Flutter component;
only an explicit no opts out. When Mobile is selected, repositories are ordered
`repo`, `web`, `mobile`, `api`, `database`, `infra`; an opt-out omits the
Mobile entry. It writes `smt.yaml` only after
confirmation and does not create a workspace. The destination file must not
already exist. Read and adjust the generated `smt.yaml` before applying it; for
example, inspect the selected repositories and add project-specific check
profiles.

```sh
$EDITOR ../platform-config/smt.yaml
bin/smt apply --config ../platform-config/smt.yaml ../platform
```

`apply` validates the supplied workspace blueprint/configuration, creates a
root repository plus one local submodule per selected component, and writes the
workspace files and local workflow metadata at a destination that does not
already exist. With Mobile selected, it creates a Git-ready `mobile-app` shell,
`mobile_worker` manifest, Flutter README and ignore rules, and a
`.tool-versions` Flutter `3.44.9` pin—not application source. It does not
invoke or require Flutter or its SDK, install dependencies, access the network,
sign an app, or publish an app. It does not create remote repositories.

Each created repository receives a scaffold-only `lefthook.yml` with top-level
`no_auto_install: true` and `assert_lefthook_installed: true`. Its `commit-msg`
entry calls bare `smt validate-message --config FILE {1}`, where `FILE` is the
correct relative path to the root `smt.yaml`. `no_auto_install` prevents
Lefthook from automatically installing or updating hooks when configuration
changes; the assertion makes Git fail if Lefthook cannot be found, rather than
silently skipping validation. Applying a blueprint does not execute Lefthook or
install a Git hook.

## Human E2E Mobile review handoff

The pending human review (`smt-3r2.5`) should create one default Mobile
blueprint (press Enter) and one explicit opt-out blueprint, then apply each in
new destinations. Verify the default YAML order and Mobile artifacts listed
above; verify the opt-out contains no Mobile stack or repository. This review
does not require Flutter installation and must not expect generated app source,
dependency installation, network access, signing, or store publication. At one
additional fresh destination, exercise one safe prerequisite, staging, Beads,
or publish failure and verify that no partial destination remains.

Add credential-free remote URLs after applying the blueprint:

```yaml
repositories:
  - id: web
    path: web-app
    remote:
      url: git@github.com:example/web-app.git
```

The generated `.gitmodules` initially records local bootstrap URLs. For
provider-backed delivery, declare exact projects and let SMT discover/create
and wire them:

```yaml
providers:
  github:
    api_base_url: https://api.github.com/
repositories:
  - id: repo
    path: .
    provider: github
    project: acme/platform
    scope: repo
    visibility: private
```

```sh
export SMT_GITHUB_TOKEN=...
bin/smt remote provision --dry-run
bin/smt remote provision --json
```

Provisioning uses child-first provider discovery/creation and private
visibility by default. It refuses incompatible existing projects or occupied
local origins, never deletes remote projects, and only updates `smt.yaml`,
`.gitmodules`, and Git origins after every target is available. Tokens are read
from `SMT_GITHUB_TOKEN` or `SMT_GITLAB_TOKEN` and are never written to disk.

## Discover commands and enable completion

```sh
bin/smt --help
bin/smt completion zsh > ~/.zfunc/_smt
```

Root help groups commands into Getting Started (`new`, `apply`), Workspace,
Review Workflow, and Developer Tools. The retained review workflow commands
are `work ready`, `review`, `review list`, `review queue`, `review requeue`,
and `release check`; use each command's `--help` for its required flags.
Completion generation and help do not load `smt.yaml`. Ensure `~/.zfunc` is in
your Zsh completion path before starting a new shell.

## Push configured repositories

```sh
bin/smt push --dry-run
bin/smt push
```

The dry run validates every root/submodule worktree and prints each current
branch in execution order without contacting a remote. A real push rejects
missing remote URLs, dirty or detached repositories, and uninitialized paths;
it pushes child repositories first and the root last. SMT never stages,
commits, force-pushes, or rolls back a successful child push.

## Create a synchronized linked worktree

```sh
bin/smt worktree add ../platform-feature --branch feature/demo --dry-run
bin/smt worktree add ../platform-feature --branch feature/demo
```

The branch must be new in every configured repository. SMT verifies clean,
attached, initialized root/submodule state plus matching root gitlinks before
creating the root worktree and then nested child worktrees. If an unexpected
child creation fails, SMT reports the created and pending paths for manual
recovery; it does not delete worktrees automatically.

## Prepare an assigned feature workspace

Resolve one active Beads feature and prepare its synchronized workspace before
starting implementation:

```sh
smt workspace prepare smt-feature ../platform-feature --branch feature/demo --dry-run
smt workspace prepare smt-feature ../platform-feature --branch feature/demo
```

Preparation selects direct active dependency-ready children with exactly one
matching `repo:<id>` label, groups them in configuration order, and records
their titles, descriptions, designs, acceptance criteria, Beads IDs, and
optional Jira-shaped aliases. It creates the root worktree before children and
writes `.smt/runs/smt-feature.json` only after all worktrees succeed. The run
manifest is ignored, contains no credentials, and is the authority for
repository ownership, check-profile names, integration gates, and accepted
commit references. The root records `ownership: integration-worker` and
`integration_gate: root`; children record `ownership: repository-worker` and
`integration_gate: root-gitlink`.

Inside that prepared workspace, commit subjects must use:

```text
feat(api): [smt-123] add endpoint
fix(web): [WEB-456] handle empty response
```

The bracketed ID is mandatory immediately after the conventional prefix.
Child commits accept only their assigned Beads ID or Jira alias. Root
integration/gitlink commits may additionally use the feature ID and assigned
root-task IDs. Missing, malformed, wrong-repository, ambiguous, or corrupt
manifest state fails closed with safe remediation. Outside a prepared
workspace, normal configured conventional-commit validation remains unchanged.

## Submit a prepared workspace

```sh
bin/smt workspace submit smt-feature --dry-run --json
bin/smt workspace submit smt-feature
bin/smt workspace submit smt-feature --ready
```

Submission selects only assigned commits ahead of each manifest base and
requires clean attached worktrees, matching configured origins, reachable
target branches, valid assigned references, and passing `submit` checks before
the first push. Child repositories are pushed before the root; a changed child
must have its gitlink integrated by a root commit. Pushes never force-update or
roll back remote state.

With a configured provider token, SMT reuses an open review with the same
source/target branches or creates a draft PR/MR. `--ready` creates or promotes
a ready review. If a token is absent, the push still succeeds and the command
prints a provider creation link with copy-ready title/body content and exact
`Closes \`WORK-ID\`` lines. A root review waits until selected child review
URLs are available. Repeating the command reuses matching open reviews.

## Traceable workspace release-gate handoff

The following checks remain human-owned release evidence. Run them in
disposable workspaces with representative root and child repositories. Do not
put tokens in commands, terminal captures, Beads notes, or committed files.

### `smt-5w0.11.7` — prepared workspace traceability

1. Create or select a real feature with one repository-scoped Beads child in
   the root and one child repository, plus one assigned Beads task with a
   Jira-shaped `external_ref`.
2. Run `smt workspace prepare FEATURE PATH --branch BRANCH` and retain the
   exact output. Inspect the ignored `.smt/runs/FEATURE.json` manifest and
   confirm the base branch and commit, repository configuration order,
   ownership, assigned Beads IDs, and Jira alias are present and secret-free.
3. In the child repository, attempt an empty commit with a missing ID and a
   commit using the other repository's assigned ID. Both must fail before a
   commit is created. Repeat with the assigned Beads ID and assigned Jira
   alias; both must succeed.
4. In the root repository, create the valid integration/gitlink commit using
   the parent feature ID. Confirm that an arbitrary valid-looking ID is
   rejected and that a corrupt, missing, or ambiguous manifest fails closed.
5. Repeat one preparation as `--dry-run` and confirm it creates no worktree or
   manifest. Record the exact commands, exit statuses, manifest inspection,
   and hook output in `smt-5w0.11.7`; agents must not close that ticket.

### `smt-5w0.13.6` — mixed-provider delivery

1. Use disposable private GitHub and GitLab projects or approved sandbox
   namespaces. Configure fully qualified projects, leave visibility private,
   and export provider tokens only in the current shell.
2. Run `smt remote provision --dry-run`, then the real command. Confirm
   child-first discovery/creation or exact compatible reuse, SSH origins,
   `.gitmodules`, persisted `remote.url` values, and safe created/existing/
   configured/pending reporting. Repeat it to verify idempotency.
3. Prepare a feature and repeat the invalid, cross-repository, assigned
   Beads, assigned Jira-alias, and root integration commit checks from the
   `.11.7` handoff. Create changes in an assigned child and the required root
   gitlink commit only.
4. Unset one provider token and run `smt workspace submit FEATURE`. Confirm
   child-first pushes succeed, the missing provider is reported as a warning,
   copy-ready review text and a safe provider link are printed, and the root
   review is deferred until child review URLs exist.
5. Restore the token and run submission again, once normally and once with
   `--ready`. Confirm draft review creation or reuse, exact `Closes` lines,
   child links in the root review, and no duplicate reviews on rerun. Record
   exact commands and review URLs without credentials; agents must not close
   `smt-5w0.13.6`.

## Inspect and install workspace hooks

```sh
# From the SMT source checkout.
task build
export PATH="$PWD/bin:$PATH"
# Return to the target/generated workspace.
cd ../platform
smt doctor
smt hooks install --dry-run
smt hooks install
smt status
smt status --json
```

The normal `status` report is for people: it has an overall label, a repository
table, configured profiles and contract counts, and safe next steps. Its JSON
form is for automation and returns the repository entries, profiles, and
contract counts. When no profiles are configured, the human report says
`profiles: none`; JSON retains `profiles: []`.

`doctor` is read-only and renders a repository-first readiness tree. Each
configured repository is expanded in configuration order with `worktree`,
`hook`, `remote`, and `provider` nodes; `tools` and `credentials` are separate
roots. It reports token presence only, never token values, and places the
remediation directly beneath the affected warning or error. Missing remotes
and provider tokens are warnings; absent providers are valid local-only state.

For example, a safe redirected report is shaped like this:

```text
DOCTOR ! WARN
workspace
├─ repo ✓ ready
│  ├─ worktree ✓ initialized
│  ├─ hook ✓ current
│  ├─ remote ✓ configured
│  └─ provider ✓ github · acme/repo
└─ api ! warning
   ├─ worktree ✓ initialized
   ├─ hook ! absent
   │  └─ fix: run smt hooks install
   ├─ remote ! not configured
   │  └─ fix: configure remote.url before remote operations
   └─ provider ✓ local-only
tools
└─ git ✓ available
credentials
└─ github ! token missing
   └─ fix: set SMT_GITHUB_TOKEN before provider operations
```

`READY` means all checks passed, `WARN` means work can continue with a
non-blocking issue, and `ERROR` means a required local check failed. A
worktree is the Git checkout SMT inspects; a hook is the commit-msg validation
installed there; a remote is the configured Git destination; a provider is the
optional GitHub/GitLab project; and a credential is the environment token used
for provider APIs. `NO_COLOR` and redirected output remain deterministic.

`task build` creates `bin/smt` in the SMT source checkout; it does not put bare
`smt` on `PATH`. Keep that inherited PATH while returning to the target
workspace. Both bare `smt` and Lefthook must remain available when Git runs a
`commit-msg` hook. Use an equivalent durable PATH setup for an IDE, GUI client,
or other launch environment; retain the bare-command design rather than
replacing it with an absolute path. `smt hooks --help` repeats that both bare
`smt` and Lefthook must be on `PATH`.

For each configured repository, `commit-msg` is `absent` when no hook exists,
`current` when it exactly matches a recognized historical SMT script or the
reviewed Lefthook 2.1.10 dispatcher, and `unmanaged` when it is custom,
lookalike, modified, symlinked, a directory, or another nonregular target. An
absent hook is a warning, not a failed `doctor` run, when the required readiness
checks pass. Unmanaged targets must be resolved manually first: SMT does not
follow or replace them and has no force or chaining mode. An exact recognized
legacy SMT hook is eligible for migration only when no `commit-msg.old` entry
exists. Lefthook 2.1.10 may then preserve it as `commit-msg.old` while it
installs its dispatcher. A current Lefthook dispatcher with an existing `.old`
entry remains allowed.

`hooks install` resolves `smt` and `lefthook` with `exec.LookPath`, then
uses argument-array `git config --get core.hooksPath` in every initialized
configured repository before any installer mutation. Any nonempty effective
setting, including a relative one, blocks all installation as a custom hook-path
policy; resolve it manually rather than forcing or resetting it. It then
requires a regular eligible `commit-msg` target and a top-level `commit-msg`
mapping in `lefthook.yml`, and runs `lefthook validate` in every repository
using argument arrays. A symlink, directory, or other nonregular `commit-msg`
target is unmanaged and blocks the plan. It completes all root-and-child
preflight before installing anything, then runs argument-array
`lefthook install commit-msg` root-first. `--dry-run` performs that preflight
and prints the configured repository plan without changing hooks. A successful
real install prints installed repository IDs. If a later real install fails,
use its installed and pending IDs for manual recovery; SMT does not force,
reset a collision, use a shell, overwrite unmanaged hooks, or undo an earlier
install.

For an exact legacy SMT `commit-msg` hook, preflight also checks
`commit-msg.old`. If any entry exists—including a symlink—both the real install
and `--dry-run` reject the whole plan before root-first execution. Lefthook
2.1.10 would refuse this migration without `--force`; resolve the collision
manually instead. The collision error does not disclose paths or hook contents.
An existing `.old` beside a current Lefthook dispatcher is allowed.

Fixture evidence is narrow: a clean fixture installed all configured hooks and
accepted a normal commit. In a deliberate negative test, removing the
installer-provided Lefthook binary while retaining `smt` on PATH caused Git to
reject an otherwise valid commit with the assertion error. Treat this as proof
of the assertion path, not as a full human E2E result.

## Build and validate

```sh
# From the SMT source checkout.
task build                         # creates bin/smt
task verify                        # runs go test ./...
export PATH="$PWD/bin:$PATH"
smt validate-message .git/COMMIT_EDITMSG
smt validate-message --config ../platform/smt.yaml .git/COMMIT_EDITMSG
```

`validate-message FILE` expects a complete commit-message file. `--config`
selects its configuration file and is useful from a child repository hook.

## Checks and contracts

```sh
bin/smt check --profile hook
bin/smt check --profile submit --repo apis --dry-run
bin/smt check --profile ci-parity --repo apis
bin/smt check --profile submit --repo apis --allow-worktree-mutation
bin/smt contracts validate
bin/smt ci audit
bin/smt ci contracts bump --id example-contract
bin/smt ci contracts bump --id example-contract --apply
```

Use `--dry-run` to inspect a check without executing it. A check that can
mutate a worktree requires the explicit `--allow-worktree-mutation` guard. A
contract bump is plan-only unless `--apply` is supplied; use an ID from the
configured CI contract report, not a placeholder.

The global flag must lead the command. It preserves machine-readable output:

```sh
bin/smt --verbose status --json > status.json
```

JSON remains on stdout in `status.json`; Logrus diagnostics go only to stderr.

Verbose check commands include timestamped, structured results for each
configured command, including the repository, profile, program, status, exit
code, duration, and captured stderr byte count. Arguments and command output
are not copied into Logrus fields.

Colors are enabled automatically for interactive terminals. Use `NO_COLOR` to
disable ANSI colors, or `CLICOLOR_FORCE=1` to force them when output is being
redirected; `NO_COLOR` takes precedence.

```sh
bin/smt --verbose check --profile hook
NO_COLOR=1 bin/smt --verbose check --profile hook
CLICOLOR_FORCE=1 bin/smt --verbose check --profile hook 2> verbose.log
```

## Release flow

```sh
task release:build VERSION=v0.1.0
ls dist/smt_v0.1.0_{linux,darwin}_{amd64,arm64}.tar.gz dist/checksums.txt
```

The version must be strict `vMAJOR.MINOR.PATCH`. The local build creates four
archives for Linux/macOS and amd64/arm64 plus `dist/checksums.txt`; it does not
tag or push.

```sh
# Mutating: verify the worktree and release decision before running this.
task release:tag VERSION=v0.1.0
```

`task release:tag` requires a fully clean worktree, runs verification and the
release build, creates an annotated tag, and pushes it to `origin`. The pushed
tag triggers `.github/workflows/release.yml`, which publishes a GitHub Release
with the four archives and checksum file. This command was not invoked during
implementation verification; no tag, push, or publication was performed.

## Related

- [[../00-project/SMT - Product Concept]]
- [[../00-project/SMT - Implementation Spec]]
- [[../../README|Repository README]]
