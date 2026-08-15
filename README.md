# smt

Sanovy Mono Tool is a small Go CLI for inspectable, repeatable work across a
Git root repository and independent submodules. The v0.1 implementation is
available locally, including an interactive blueprint-and-apply workspace flow,
configured
repository pushes, synchronized linked worktrees, status/diagnostics, check
profiles, contract validation, CI contract auditing, guarded bumps, and release
packaging.

## Minimal onboarding

Prerequisites: Git, Go, Task, and Lefthook. Generated Git hooks deliberately
invoke bare `smt` through Lefthook, so both `smt` and `lefthook` must be
durably available on `PATH` in every hook-running environment.

```sh
# From the SMT source checkout.
task build
task verify
mkdir -p ../platform-config
bin/smt new ../platform-config/smt.yaml
# Inspect and edit ../platform-config/smt.yaml as needed.
bin/smt apply --config ../platform-config/smt.yaml ../platform
```

`smt new` requires a new blueprint file. It prompts for the fixed Next.js, Go,
PostgreSQL, and Docker/OpenTofu components, and asks whether to include a
Flutter Mobile application immediately after the Web selection. Mobile targets
Android and iOS, defaults to included when you press Enter, and is excluded
only by an explicit no. When Mobile is selected, repositories are ordered
`repo`, `web`, `mobile`, `api`, `database`, `infra`; an opt-out omits the
Mobile entry. Inspect and edit the resulting `smt.yaml`
before `smt apply` creates the root repository and selected local submodules at
the new destination. A selected Mobile component is a Git-ready `mobile-app`
shell with a `mobile_worker` manifest, Flutter README and ignore rules, and
`.tool-versions` pinning Flutter `3.44.9`; it is not generated Flutter app
source. Applying it does not invoke or require the Flutter CLI or SDK, install
dependencies, access the network, sign an app, or publish to an app store. Add
credential-free `remote.url` values after applying the blueprint and before
using
`bin/smt push [--dry-run]`. Create a matching root-plus-submodule workspace
with `bin/smt worktree add PATH --branch NAME [--dry-run]`.

In the SMT source checkout, build the local CLI and make its bare command
available. Then return to the target workspace to diagnose or install hooks:

```sh
# From the SMT source checkout.
task build
export PATH="$PWD/bin:$PATH"
# Return to the target/generated workspace.
cd ../platform
smt doctor
smt hooks install --dry-run
smt hooks install
```

`task build` creates `bin/smt` in the SMT source checkout; it does not change
`PATH`. The generated root and child `lefthook.yml` files have top-level
`no_auto_install: true` and `assert_lefthook_installed: true`, and deliberately
invoke bare `smt validate-message --config FILE {1}` with the correct relative
root configuration. `no_auto_install` prevents Lefthook from automatically
installing or updating hooks when configuration changes; the assertion makes
Git reject the hook if Lefthook cannot be found, rather than silently skipping
commit-message validation. Keep both `smt` and Lefthook available on
the equivalent durable PATH used by IDE or GUI launchers; do not substitute a
fragile absolute path. `smt hooks --help` also calls out the two PATH
requirements.

`doctor` always checks Git, bare `smt`, and Lefthook. It explains how to build
and expose `smt`, or install Lefthook and rerun it, before recommending hook
installation. An absent hook is a warning rather than a failed doctor run when
the other readiness checks pass. `status --json` is the machine-readable
version of the human status report. With no configured profiles, the human
report writes `profiles: none`; JSON keeps the machine-readable empty array,
`profiles: []`. Commit-message hooks are `absent`, `current`, or `unmanaged`;
custom, lookalike, modified, symlinked, directory, and other nonregular hook
targets are never followed or overwritten.

`smt hooks install` resolves both tool names with PATH lookup, then checks
every initialized configured repository with argument-array
`git config --get core.hooksPath`. Any nonempty effective setting—including a
relative path—is a custom hook-path policy that blocks the entire install plan;
resolve it manually rather than forcing or resetting it. The same all-repository
preflight requires a regular eligible `commit-msg` target, a `commit-msg`
mapping in every `lefthook.yml`, and a successful argument-array
`lefthook validate`. It finishes before changing anything, then runs
argument-array `lefthook install commit-msg` root-first.

Unmanaged hooks, including custom, lookalike, modified, symlinked, directory,
and other nonregular targets, are never followed or replaced. An exact
legacy SMT hook is the narrow migration exception, but only when no
`commit-msg.old` entry exists. Lefthook 2.1.10 may then preserve the hook as
`commit-msg.old` while installing its dispatcher. If any `.old` entry already
exists, including a symlink, both `smt hooks install` and `--dry-run` reject the
whole plan before root-first execution; resolve that collision manually. A
current Lefthook dispatcher with an existing `.old` entry remains allowed.
Collision errors do not expose paths or hook contents. This never permits
`--force`, resetting a collision, a shell, overwriting unmanaged hooks, or
rollback. A successful run prints installed repository IDs; a later failure
reports installed and pending IDs for manual recovery. `smt apply` only writes
the Lefthook scaffold—it never executes Lefthook or installs hooks.

Fixture evidence is deliberately narrow: a clean fixture installed hooks in all
configured repositories and accepted a normal commit. In a controlled negative
test, removing the installer-provided Lefthook binary while leaving `smt` on
PATH caused Git to reject an otherwise valid commit with Lefthook's assertion
error. This is evidence for the assertion boundary, not a substitute for human
end-to-end review in every launch environment.

For the active Beads lifecycle, run `smt prepare` (no arguments), then
`smt switch BEAD_ID` and `smt pull` as needed. Preparation creates and reports
the open `Prepared workspace` task before complete preflight; a failed
preflight leaves that task open and makes no Git mutation. It stashes tracked
and untracked changes and leaves ignored files untouched. Switching uses only
an existing local Beads-ID branch and never auto-pops or rolls back. Pull is
child-first and fast-forward-only. The effective default branch is repository
`remote.default_branch`, then `main`.
Default branches use ordinary conventional commits; non-default active Beads
branches require the exact branch ID as `type(scope): [BEAD-ID] summary`.
Hooks require Beads readiness.

`bin/smt --help` groups commands into Getting Started, Workspace, Review
Workflow, and Developer Tools. The retained surface includes workspace
inspection and Git operations, check/contract/CI tools, work and review
workflow commands, and release readiness. Generate shell completion with, for
example, `bin/smt completion zsh`; completion and help work without an
`smt.yaml` file.

For copyable command examples, configuration assumptions, and the safe release
flow, see [SMT Command Recipes](docs/10-development/SMT%20-%20Command%20Recipes.md).
The implementation contract remains [SMT Implementation Spec](docs/00-project/SMT%20-%20Implementation%20Spec.md).

In the SMT source checkout, `task build` creates `bin/smt`; `task verify` runs
the Go test suite. Release tagging is intentionally mutating:
`task release:tag VERSION=vX.Y.Z` requires a fully clean worktree, verifies and
builds, creates an annotated tag, and pushes it to `origin`. The pushed tag
triggers GitHub Actions to publish the four archives and `checksums.txt` as a
GitHub Release. It was not run during this implementation work.

The repository uses `smt.yaml` for workspace configuration. Tokens, when
needed by future provider integrations, must remain runtime-only and must
never be printed or committed.
