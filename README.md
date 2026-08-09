# smt

Sanovy Mono Tool is a small Go CLI for inspectable, repeatable work across a
Git root repository and independent submodules. The v0.1 implementation is
available locally, including an interactive blueprint-and-apply workspace flow,
configured
repository pushes, synchronized linked worktrees, status/diagnostics, check
profiles, contract validation, CI contract auditing, guarded bumps, and release
packaging.

## Minimal onboarding

Prerequisites: Go, Task, and (for hook installation) Lefthook.

```sh
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

`bin/smt --help` groups commands into Getting Started, Workspace, Review
Workflow, and Developer Tools. The retained surface includes workspace
inspection and Git operations, check/contract/CI tools, work and review
workflow commands, and release readiness. Generate shell completion with, for
example, `bin/smt completion zsh`; completion and help work without an
`smt.yaml` file.

For copyable command examples, configuration assumptions, and the safe release
flow, see [SMT Command Recipes](docs/10-development/SMT%20-%20Command%20Recipes.md).
The implementation contract remains [SMT Implementation Spec](docs/00-project/SMT%20-%20Implementation%20Spec.md).

`task build` creates `bin/smt`; `task verify` runs the Go test suite. Release
tagging is intentionally mutating: `task release:tag VERSION=vX.Y.Z` requires a
fully clean worktree, verifies and builds, creates an annotated tag, and pushes
it to `origin`. The pushed tag triggers GitHub Actions to publish the four
archives and `checksums.txt` as a GitHub Release. It was not run during this
implementation work.

The repository uses `smt.yaml` for workspace configuration. Tokens, when
needed by future provider integrations, must remain runtime-only and must
never be printed or committed.
