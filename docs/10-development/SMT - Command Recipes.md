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
updated: 2026-07-30
---
# SMT — Command Recipes

These examples assume commands run from the repository root, `smt.yaml` is
present and valid, and Go plus Task are installed. Build first when using
`bin/smt`.

## Create a platform workspace

```sh
bin/smt init ../platform
```

`init` interactively selects the fixed Next.js, Go, PostgreSQL, Docker/OpenTofu,
and Codex profiles. It creates a root repository, one local submodule per
selected component, `smt.yaml`, ignore files, and a repository-local Codex
manager/worker/documentation workflow. It does not install dependencies or
create remote repositories.

Add credential-free remote URLs after initialization:

```yaml
repositories:
  - id: web
    path: web-app
    remote:
      url: git@github.com:example/web-app.git
```

The generated `.gitmodules` records local bootstrap URLs. SMT pushes through
`remote.url` but does not yet synchronize those values into `.gitmodules` for
fresh external clones.

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

## Build and validate

```sh
task build                         # creates bin/smt
task verify                        # runs go test ./...
bin/smt validate-message .git/COMMIT_EDITMSG
bin/smt status
bin/smt status --json
bin/smt doctor
```

`validate-message FILE` expects a complete commit-message file. `status` reads
configured repositories and contracts; `doctor` checks local prerequisites.

## Hooks and commit task

These tasks require Task and Lefthook. Build `bin/smt` first; `task setup`
installs the Lefthook `commit-msg` hook, which delegates to SMT validation.

```sh
task hooks:install
task setup
task commit:validate -- .git/COMMIT_EDITMSG
```

`commit:validate` expects an existing complete commit-message file.

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
