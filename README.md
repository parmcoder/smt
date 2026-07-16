# smt

Sanovy Mono Tool is a small Go CLI for inspectable, repeatable work across a
Git root repository and independent submodules. The v0.1 implementation is
available locally, including status/diagnostics, check profiles, contract
validation, CI contract auditing, guarded bumps, and release packaging.

## Minimal onboarding

Prerequisites: Go, Task, and (for hook installation) Lefthook.

```sh
task build
task verify
bin/smt status
bin/smt doctor
```

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
