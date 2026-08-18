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
updated: 2026-08-17
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

`new` interactively selects independent Web (`nextjs`), Mobile (`flutter`), API
(`go`), and Database (`postgresql`) components. Immediately after Web, it asks
`Include Flutter mobile application? [Y/n]`: Enter includes the Android/iOS
Flutter component; only an explicit no opts out. When Mobile is selected,
repositories are ordered `repo`, `web`, `mobile`, `api`, `database`; an opt-out
omits the Mobile entry. New blueprints have no DevOps prompt,
`workspace.stack.devops`, `infra` repository, or Docker/OpenTofu metadata or
artifacts. After Database, it offers the optional default-no
`Include E2E quality declaration? [y/N]` question. Opting in records only
`modules: [e2e]` on the root; component repositories receive exact module IDs,
and no E2E repository or scaffold is created. It writes `smt.yaml` only after
confirmation and does not create a workspace. The generated file carries the
exact provenance mapping documented in [[../00-project/SMT - Implementation Spec#Configuration contract|the implementation
specification]], with no timestamp, user, machine/path, Git
SHA, random value, or environment-derived field. Generation is offline and
byte-stable for identical selections in fresh destinations. The destination
file must not already exist. Inspect the generated `smt.yaml` before applying
it.

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
Legacy DevOps-shaped configurations are rejected before destination mutation;
remove the legacy entries and regenerate the blueprint. Generated blueprints
must carry the exact supported provenance; missing, unsupported, or unknown
provenance fails before service or destination mutation. A general
non-generated version-1 configuration without provenance remains usable for
lifecycle and diagnostic commands, but is not applyable as a new generated
blueprint. An existing destination file or directory is refused without
overwrite, merge, regeneration, upgrade, or `smt extend` execution. This
release does not provide runnable Web/API/Mobile templates, Podman/Compose
artifacts, platform repositories/scaffolds/runtime artifacts, a remote module
registry, or `smt extend`. Generated module annotations are persisted, but apply does not
execute their verification recipes, install referenced tools/skills/MCP,
mutate host configuration, or create module repositories.

### Inspect module declarations

The implementation specification documents the static schema-v1 catalog and
the exact generated annotations. The implemented catalog is code-owned, not
user YAML, and contains exactly 11 declarations: selectable `web`, `mobile`,
`api`, `database`, and `e2e`; and non-selectable platform declarations
`container`, `cicd`, `observability`, `iac`, `k8s`, and `argocd`.

Placement modes are declarative and validated as `attached`, `shared`, or
`independent`. The `.5` catalog uses this authoritative matrix:

| Module(s) | Placement mode and targets | Stable completion criterion IDs |
| --- | --- | --- |
| `web`, `mobile`, `api`, `database` | independent self-targets | `web.declaration`, `mobile.declaration`, `api.declaration`, `database.declaration` |
| `e2e` | attached to `repo` | `e2e.declaration` |
| `container` | attached to `web` + `api` | `container.declaration` |
| `cicd` | attached to `repo` + `web` + `mobile` + `api` + `database` | `cicd.repository-boundary` |
| `observability` | attached to `web` + `api` + `database` | `observability.boundary` |
| `iac` | independent at `platform/iac` | `iac.provider-neutral` |
| `k8s` | independent at `platform/k8s` | `k8s.static-validation` |
| `argocd` | independent at `platform/argocd`; requires `k8s` | `argocd.sync-policy` |

The full matrix also records each declaration's path and scope in [[../00-project/SMT - Implementation Spec|the implementation specification]]. Catalog validation covers schema, duplicate/unknown module and capability references, safe paths, placement targets, stable completion IDs, and capability dependency cycles. Configuration validation rejects unknown or duplicate repository module IDs and missing required capabilities.

`Config.LoadBytes` accepts known non-selectable platform metadata when the
references and dependencies are valid, so `[argocd, k8s]` is loadable while
`[argocd]` is rejected for its missing capability. `Apply` and
`ValidateBlueprint` reject non-selectable platform metadata before topology
checks or staging/destination mutation. The root-only `modules: [e2e]`
declaration remains metadata; it does not create an E2E repository, scaffold,
or artifact.

This `.5` slice adds declarations and validation only. Apply does not execute
verification commands, install tools/skills/MCP, mutate host configuration,
create platform repositories/scaffolds/runtime artifacts, or run
Compose/Podman/Kubernetes/ArgoCD/OpenTofu. Runnable starters and `smt extend`
remain deferred.

Each created repository receives a scaffold-only `lefthook.yml` with top-level
`no_auto_install: true` and `assert_lefthook_installed: true`. Its `commit-msg`
entry calls bare `smt validate-message --config FILE {1}`, where `FILE` is the
correct relative path to the root `smt.yaml`. `no_auto_install` prevents
Lefthook from automatically installing or updating hooks when configuration
changes; the assertion makes Git fail if Lefthook cannot be found, rather than
silently skipping validation. Applying a blueprint does not execute Lefthook or
install a Git hook.

### Beads bootstrap files

The initial workspace commit includes Beads configuration and metadata while
honoring the `.beads/.gitignore` created by `bd init`. The embedded Dolt
database, locks, backups, and other local runtime files remain on disk but are
not tracked by Git. Verify a generated workspace with:

```sh
git ls-files -ci --exclude-standard
git check-ignore -v .beads/embeddeddolt/
```

For a workspace created by an older SMT version, preserve the local Beads
database and remove only indexed ignored paths from Git:

```sh
bd doctor --fix
git ls-files -ci --exclude-standard
git rm --cached -r .beads/embeddeddolt/
git rm --cached .beads/.local_version
git commit -m "fix(repo): stop tracking Beads runtime data"
```

Use the output of `git ls-files -ci --exclude-standard` to include any other
indexed ignored runtime paths; do not delete the local files or rewrite Git
history.

## Human E2E Mobile review handoff

The pending human review (`smt-3r2.5`) should create one default Mobile
blueprint (press Enter) and one explicit opt-out blueprint, then apply each in
new destinations. Verify the default YAML order and Mobile artifacts listed
above, including the exact provenance mapping from the implementation spec;
verify the opt-out contains no Mobile stack or repository. Repeat identical
selections in fresh destinations and compare bytes to confirm deterministic
offline generation. This review
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

## Discover commands and create Beads tickets

```sh
bin/smt --help
bd prime
bd create --title="Short task title" --description="Why this exists and what needs to be done" --type=task --priority=2
bd show <id>
bd update <id> --claim
bd ready
bd blocked
bd close <id> --reason="Completed"
```

Agents create and manage feature or task tickets directly with Beads; SMT does
not wrap ticket creation, review queues, release readiness, or ready-work
listing. Create the implementation ticket before editing code. Use `smt
prepare` only for repository lifecycle coordination; it may create its special
internal `Prepared workspace` task.

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

## Beads branch lifecycle

```sh
bin/smt prepare
bin/smt switch
bin/smt switch smt-123
bin/smt pull
```

`prepare` has no positional arguments, creates and reports the open `Prepared
workspace` task before running complete preflight, and leaves that task open
when preflight fails without mutating Git. It stashes tracked and untracked
changes but leaves ignored files in place. `switch` with no argument returns
every repository to its effective default branch; `switch BEAD_ID` uses only an
existing local branch. Neither form creates, auto-pops, or rolls back. `pull`
fast-forwards child repositories before the root. The effective default branch
is per-repository `remote.default_branch`, then `main`.
Default branches use ordinary conventional-commit syntax; non-default active
Beads branches require the exact branch ID as `type(scope): [BEAD-ID] summary`.
The root has no special manifest exception. Hooks require Beads readiness.

The former workspace `prepare/submit` manifest flow, Jira aliases, assignment
waves, and provider review automation are removed from the active CLI.

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

<!-- INACTIVE HISTORICAL WORKSPACE PREPARE/SUBMIT AND PROVIDER REVIEW RECIPES
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

## Inspect and install workspace hooks -->

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

## Profiles and contracts

Profiles and reusable contracts remain valid in `smt.yaml` for configuration
and diagnostics. `smt status` and `smt doctor` summarize profile names and
contract counts. The former standalone check, contract-validation, CI-audit,
and guarded-bump command surfaces have been retired; use direct Beads tickets
for work that needs follow-up.

The global flag must lead the command. It preserves machine-readable output:

```sh
bin/smt --verbose status --json > status.json
```

JSON remains on stdout in `status.json`; Logrus diagnostics go only to stderr.

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
