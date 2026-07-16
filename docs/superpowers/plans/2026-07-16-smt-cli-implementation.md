# SMT CLI v0.1.0 GitHub Release Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prepare the first `smt` CLI release workflow so `release:build` creates four local platform archives plus SHA-256 checksums, and `release:tag VERSION=vX.Y.Z` creates and pushes the tag that triggers GitHub Release publication.

**Architecture:** Keep release orchestration in the root `Taskfile.yml` and GitHub Actions. `release:build` deterministically creates all local release assets, and the intentionally mutating `release:tag` task validates/builds, creates an annotated tag, then pushes that tag. GitHub Actions is the only publisher and runs from the pushed semantic-version tag. CLI logging uses Logrus, with `--verbose` enabling diagnostic messages on stderr while normal command output remains stable on stdout.

**Tech Stack:** Go 1.26.5+, standard library, `github.com/sirupsen/logrus`, Taskfile, GitHub Actions, GitHub CLI/release action as selected by the repository’s existing workflow conventions, and tar/zip plus `sha256sum`/`shasum`-compatible tooling.

## Global Constraints

- Scope is v0.1.0 release preparation and tag initiation: implement local release builds and the explicit `release:tag` tag-and-push task; do not publish directly from the local Taskfile.
- Preserve existing user changes; modify only the paths listed by each task and do not stage, revert, reset, or rewrite unrelated work.
- The four release targets are `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`.
- Every archive contains the executable named `smt`; archive names are `smt_<version>_<os>_<arch>.tar.gz`.
- Publish one `checksums.txt` containing SHA-256 entries for all four archives.
- The workflow triggers only on pushed tags matching `v*.*.*`; it creates a GitHub Release and attaches all archives plus `checksums.txt`.
- No credentials, tokens, authorization headers, or sensitive payloads may be logged or committed.
- `--verbose` writes Logrus diagnostics to stderr; ordinary command results stay on stdout and verbose mode must not change exit status.

---

## File map and ownership

| Path | Responsibility | Owner |
| --- | --- | --- |
| `Taskfile.yml` | Local four-target build/checksum task and explicit tag-and-push release initiator | `backend_worker` |
| `cmd/smt/main.go` and command files | Global `--verbose` parsing and Logrus setup | `backend_worker` |
| `cmd/smt/*_test.go` | CLI logging and flag regression tests | `backend_worker` |
| `.github/workflows/release.yml` | Pushed-tag matrix build, archive/checksum generation, Release creation | `backend_worker` |
| `.github/workflows/release_test.go` or repository-native workflow contract test path | Static release workflow assertions | `backend_worker` |
| `README.md` and `docs/` release notes | Local commands and release-consumer documentation only | `doc_writer` after behavior acceptance |

Do not modify `smt.yaml`, provider integrations, submit orchestration, cloud/deployment files, or unrelated package behavior.

## Task 1: Define local build and release-tag helpers

**Files:** Modify `Taskfile.yml`; add focused Taskfile contract coverage only if the repository already has a native Taskfile test mechanism.

- [ ] **Step 1: Specify the task contract.** Keep `task build` producing `bin/smt`. Add `task release:build VERSION=vX.Y.Z`, which validates the version and produces all four archives in `dist/`: `smt_<VERSION>_linux_amd64.tar.gz`, `smt_<VERSION>_linux_arm64.tar.gz`, `smt_<VERSION>_darwin_amd64.tar.gz`, and `smt_<VERSION>_darwin_arm64.tar.gz`, plus `dist/checksums.txt`. Add `task release:tag VERSION=vX.Y.Z`, which requires a clean worktree, runs the full local verification and `release:build`, creates annotated tag `vX.Y.Z`, then pushes that tag to `origin`.
- [ ] **Step 2: Implement deterministic artifact generation and initiation.** `release:build` builds every target with `CGO_ENABLED=0`, `-trimpath`, and `-ldflags "-X main.version=<version>"` if the CLI already exposes a version variable; otherwise do not invent runtime version behavior. Package only the `smt` executable, use the stable archive names above, and generate `checksums.txt` with SHA-256 entries relative to `dist`. `release:tag` must fail before creating a tag if validation, build, archive/checksum generation, clean-worktree validation, or tag-existence validation fails; when these pass, use `git tag -a <VERSION> -m "Release <VERSION>"` and `git push origin <VERSION>`.
- [ ] **Step 3: Verify locally without mutation.** Run `task build`, `task release:build VERSION=v0.1.0-test`, inspect all four archives and `dist/checksums.txt`, and run `git diff --check`. Do not run `task release:tag`; confirm verification created no Git tag, push, or remote API call.

## Task 2: Add Logrus-backed `--verbose` stderr behavior

**Files:** Modify the CLI entrypoint/command files; add or modify `cmd/smt/*_test.go`.

- [ ] **Step 1: Add failing tests.** Assert `smt --verbose <command>` is accepted before the subcommand, diagnostics are emitted through a Logrus logger configured for stderr, normal success output remains on stdout, and non-verbose output does not contain debug diagnostics. Add an error-path test proving verbose mode preserves the existing nonzero exit code.
- [ ] **Step 2: Implement the smallest wiring change.** Parse the global flag before dispatch, configure Logrus once with stderr as its output, and use debug-level diagnostics for verbose-only details. Do not move user-facing command results from stdout or log secrets.
- [ ] **Step 3: Verify.** Run the focused CLI tests, `go test ./cmd/smt -count=1`, and `gofmt` on changed Go files.

## Task 3: Add pushed-tag GitHub Release workflow

**Files:** Create `.github/workflows/release.yml`; create the repository-native workflow contract test path if no existing convention exists.

- [ ] **Step 1: Write failing contract tests.** Read the workflow as text/YAML and assert: trigger `push.tags` matches `v*.*.*`; a matrix contains exactly `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`; the version comes from `github.ref_name`; builds are CGO-disabled and reproducible; four archives and one `checksums.txt` are generated; and a release step uploads all five assets.
- [ ] **Step 2: Implement the workflow.** Check out the repository, set up the pinned Go toolchain compatible with `go.mod`, build each matrix target, package `smt_<version>_<os>_<arch>.tar.gz`, upload matrix artifacts, merge them in a release job, generate SHA-256 checksums, and create the GitHub Release for the pushed tag with the four archives and checksum file attached. Use least-privilege `contents: write` only on the publishing job.
- [ ] **Step 3: Verify workflow structure.** Run the contract test and the repository’s available YAML parser/linter. If GitHub-hosted execution cannot be reproduced locally, record that as unverified; do not simulate a publish with a real tag or token.

## Task 4: Final verification and documentation handoff

**Files:** Modify only `README.md`/the relevant `docs/` note after Tasks 1–3 are accepted.

- [ ] **Step 1: Run the acceptance checks without initiating a release.** Run `gofmt -w` on changed Go files, `go test ./... -count=1`, `task build`, `task release:build VERSION=v0.1.0-test`, and `git diff --check`. Do not run `task release:tag` as part of implementation verification.
- [ ] **Step 2: Inspect the diff and assets.** Confirm only the planned files changed; all four archives and `checksums.txt` exist and agree with the workflow; no tag/push/release command ran; no credentials are present; and `--verbose` diagnostics use stderr.
- [ ] **Step 3: Update documentation.** Document `task release:build VERSION=vX.Y.Z` as the non-mutating local validation/build command, and `task release:tag VERSION=vX.Y.Z` as the intentional annotated-tag-and-push command that initiates GitHub publishing. Do not claim that tagging or publishing happened in this implementation task.

## Explicit non-scope removed from this plan

Do not add future/provider/submit/release-product work here: GitLab/GitHub MR or PR providers, credentials, mixed-provider orchestration, checkout/submit workflows, changesets, release planning, cloud/database actions, deployment/rollback, YAML selector rewrites, automatic CI edits, or provider-native job execution. Those items remain outside this v0.1.0 release-preparation task rather than being marked complete.

## Completion report

Report changed paths, exact checks and results, assumptions, unresolved risks, and unverified behavior. Explicitly report that implementation verification did not invoke `task release:tag`, so no actual tag was created, pushed, or published. Leave the worktree unstaged and preserve all pre-existing changes.
