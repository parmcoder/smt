# SMT CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the first tagged `smt` Go CLI: safe checkout, hook installation, strict commit validation, ordered submit, GitLab/GitHub review requests, and CI range validation for a configured root repository and independent submodules.

**Architecture:** `cmd/smt` owns flag parsing, dependency construction, exit status, and output. Small `internal` packages own configuration, commit policy, Git execution, checks, hooks, review-provider adapters, and submit orchestration; they communicate through interfaces that use contexts and argument arrays. Submit plans all local preflight before any commit, commits submodules before the root gitlink, then stops safely on the first remote failure without rollback.

**Tech Stack:** Go 1.26.4+, Go standard library, `gopkg.in/yaml.v3`, `github.com/leodido/go-conventionalcommits`, `gitlab.com/gitlab-org/api/client-go/v2`, `github.com/google/go-github/v89/github`, Git, GitLab CI, and GitHub Actions.

## Global Constraints

- The committed configuration is `smt.yaml`; it contains no credentials and `version` is exactly `1`.
- Add only the four application dependencies named in the implementation specification.
- Run Git and configured checks with context-aware argument arrays; never pass a shell command string to a shell.
- Read tokens only from `SMT_GITLAB_TOKEN` and `SMT_GITHUB_TOKEN`; never save, log, print, or place them in test fixtures.
- Checkout preflights every configured repository and does not switch any repository until all preflight and branch resolution succeeds.
- Submit validates the complete message before Git mutation, preflights every changed repository before any commit, commits changed submodules before the root, and never commits a repository with no staged result.
- `--dry-run` validates and reports a plan but runs no formatter, check, Git write, push, or review-provider API call.
- On remote failure, stop without reset, force-push, branch deletion, MR/PR closure, or any destructive remote rollback; print completed actions and the exact safe retry command.
- `work_manager` delegates exactly one Go task at a time to `backend_worker`, reviews its integrated diff, and sends blocking remediation only to that worker. `work_manager` does not write Go. `doc_writer` aligns high-level docs and prompts only after behavior is accepted.

---

## Package and file map

| Path | Responsibility | Owner |
| --- | --- | --- |
| `internal/config/config.go`, `load.go` | YAML types, workspace-relative path resolution, semantic validation | `backend_worker` |
| `internal/commit/policy.go` | Strict full-message parse and allowed type/scope policy | `backend_worker` |
| `internal/git/runner.go`, `repository.go`, `checkout.go` | Context-aware Git argument execution, state discovery, branch planning/execution | `backend_worker` |
| `internal/checks/runner.go` | Argument-array command checks and changed SQL formatting | `backend_worker` |
| `internal/hooks/hooks.go` | Managed hook templates, backup policy, install discovery | `backend_worker` |
| `internal/review/provider.go`, `gitlab.go`, `github.go` | Normalized request types and provider-specific MR/PR APIs | `backend_worker` |
| `internal/submit/service.go` | Changed-repository discovery, preflight, commits, push/review ordering, recovery summary | `backend_worker` |
| `cmd/smt/*.go` | CLI parsing, dependency wiring, command output and exit mapping | `backend_worker` |
| `.gitlab-ci.yml`, `.github/workflows/commit-validate.yml` | Pinned tagged-module range validation | `backend_worker` only if explicitly assigned non-Go CI paths; otherwise `doc_writer` may document but not alter behavior |
| `README.md`, `docs/`, `prompts/` | Installation, hook recovery, CI pinning, behavior documentation | `doc_writer` |

## Execution protocol for every task

1. `work_manager` sends `backend_worker` one decision-complete assignment: exact owned/non-owned paths, interfaces below, tests, commands, and invariant reminders.
2. The worker writes the named failing tests, records the expected failing command, implements only the assigned paths, and runs the named passing commands.
3. The worker returns changed paths, check results, assumptions, unresolved risks, and unverified behavior. It does not delegate.
4. `work_manager` inspects the integrated diff for the named interface, token hygiene, no shell strings, ordering, and regression coverage. Blocking findings return only to that worker.
5. After accepted behavior, `doc_writer` updates only the associated high-level documentation/prompt contract. `work_manager` then continues with the next serial backend task.

### Task 1: Module baseline and configuration loader

**Files:**
- Modify: `go.mod`, `smt.yaml`
- Create: `internal/config/config.go`, `internal/config/load.go`, `internal/config/config_test.go`

**Interfaces:**

```go
package config

type Provider string
const (GitLab Provider = "gitlab"; GitHub Provider = "github")
type Check struct { Kind string `yaml:"kind"`; Argv []string `yaml:"argv"`; Include []string `yaml:"include"` }
type Repository struct { ID string `yaml:"id"`; Path string `yaml:"path"`; Provider Provider `yaml:"provider"`; Project string `yaml:"project"`; Scope string `yaml:"scope"`; Checks []Check `yaml:"checks"` }
type GitLabProvider struct { APIBaseURL string `yaml:"api_base_url"` }
type GitHubProvider struct { EnterpriseBaseURL string `yaml:"enterprise_base_url"`; EnterpriseUploadURL string `yaml:"enterprise_upload_url"` }
type Providers struct { GitLab GitLabProvider `yaml:"gitlab"`; GitHub *GitHubProvider `yaml:"github"` }
type CommitPolicy struct { Types []string `yaml:"types"`; Scopes []string `yaml:"scopes"` }
type Config struct { Version int `yaml:"version"`; Providers Providers `yaml:"providers"`; Commit CommitPolicy `yaml:"commit"`; Repositories []Repository `yaml:"repositories"` }
type WorktreeInspector interface { IsWorktree(context.Context, string) (bool, error) }
func Load(path string) (Config, string, error) // returns structural validation and absolute workspace root
func ValidateWorktrees(context.Context, Config, string, WorktreeInspector) error
```

- [ ] **Step 1: Write failing configuration tests**

Create table tests using temporary workspace directories for: valid five-repository YAML; duplicate `id`; duplicate cleaned path; duplicate `scope`; no `path: .`; path escaping with `../outside`; unknown provider; GitHub project without exactly one slash; empty command `argv`; `sql-format` with argv other than `[pg_format]`; and `sql-format` without `include`. Use a fake `WorktreeInspector` to test initialized and non-worktree repository paths through `ValidateWorktrees`. Assert each error names the offending field or repository.

- [ ] **Step 2: Run the configuration tests to verify failure**

Run: `go test ./internal/config -run 'TestLoad' -count=1`

Expected: FAIL because package and `Load` do not exist.

- [ ] **Step 3: Implement YAML decoding and validation**

Decode only `smt.yaml` with `yaml.v3`; use `filepath.Abs`, `filepath.Rel`, and `filepath.EvalSymlinks` to reject paths outside the workspace. Keep structural validation in `Load` and call `ValidateWorktrees` through the injected `WorktreeInspector` rather than inferring repositories from remotes. Require version `1`, exactly one root, unique `id/path/scope`, configured scope membership, providers `gitlab|github`, non-empty project, URL-encoded GitLab namespace/project, and GitHub `owner/repository`. Validate checks as exact argument arrays and never invoke a shell.

- [ ] **Step 4: Run tests and format**

Run: `gofmt -w internal/config && go test ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review that validation does not read tokens or remotes. Commit only after review:

```sh
git add go.mod go.sum smt.yaml internal/config
git commit -m "feat(repo): add SMT configuration validation"
```

### Task 2: Strict Conventional Commit policy and range validation support

**Files:**
- Create: `internal/commit/policy.go`, `internal/commit/policy_test.go`

**Interfaces:**

```go
package commit
type Policy struct { Types map[string]struct{}; Scopes map[string]struct{} }
type Message struct { Subject, Type string; Scopes []string; Raw string }
func NewPolicy(types, scopes []string) Policy
func (Policy) Validate(raw string) (Message, error)
```

- [ ] **Step 1: Write failing parser-policy tests**

Table-test every allowed type with `type(api): subject`; valid body and footer; valid `feat(api,web): subject`; parser-invalid body/footer separation; unsupported `style(api): subject`; no scope; `feat(): subject`; `feat(api,,web): subject`; and `feat(unknown): subject`. Assert errors include valid types and scopes where applicable and never echo unrelated message body content.

- [ ] **Step 2: Run the focused tests to verify failure**

Run: `go test ./internal/commit -run 'TestPolicyValidate' -count=1`

Expected: FAIL because `Policy.Validate` does not exist.

- [ ] **Step 3: Implement strict full-message parsing**

Use `go-conventionalcommits` strict parsing and `TypesConventional`, validate the parser result against `Policy`, split only the parsed optional scope on commas, reject blank members, and return a structured `Message` whose `Subject` is the first line.

- [ ] **Step 4: Run the tests and static package verification**

Run: `gofmt -w internal/commit && go test ./internal/commit -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review full-message parsing and scope errors. Commit:

```sh
git add internal/commit go.mod go.sum
git commit -m "feat(repo): validate scoped commit messages"
```

### Task 3: Context-aware Git runner and repository state

**Files:**
- Create: `internal/git/runner.go`, `internal/git/repository.go`, `internal/git/runner_test.go`, `internal/git/repository_test.go`

**Interfaces:**

```go
package git
type Result struct { Stdout, Stderr string; ExitCode int }
type Runner interface { Run(context.Context, string, ...string) (Result, error) }
type Repository struct { ID, Dir string; IsRoot bool }
type State struct { Branch string; Detached, Dirty, Initialized bool; ChangedFiles []string }
type CommitMessage struct { SHA, Message string }
type Inspector struct { Runner Runner }
func (Inspector) IsWorktree(context.Context, string) (bool, error)
func Inspect(context.Context, Runner, Repository) (State, error)
func ChangedFiles(context.Context, Runner, Repository) ([]string, error)
func CommitMessages(context.Context, Runner, Repository, from, to string) ([]CommitMessage, error)
```

- [ ] **Step 1: Write failing runner and state tests**

Use a recording fake runner to assert exact arrays such as `git -C <dir> status --porcelain=v1 --untracked-files=all`, `symbolic-ref --quiet --short HEAD`, and `diff --name-only --diff-filter=ACMRTUXB`. Add temporary local Git-repository tests proving dirty index/worktree detection, detached HEAD detection, and ignored files excluded from changed files.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/git -run 'Test(Inspect|ChangedFiles|Runner)' -count=1`

Expected: FAIL because the package types are absent.

- [ ] **Step 3: Implement argument-array execution and state queries**

Implement `Runner` with `exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)`, capture stdout/stderr, and wrap failures with operation and repository directory but never environment values. Determine initialized status with `rev-parse --is-inside-work-tree`; never use shell strings or `sh -c`.

- [ ] **Step 4: Run package tests**

Run: `gofmt -w internal/git && go test ./internal/git -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review every runner call for `context.Context` and argument arrays. Commit:

```sh
git add internal/git
git commit -m "feat(repo): add safe Git repository runner"
```

### Task 4: Checkout planner and all-or-nothing executor

**Files:**
- Create: `internal/git/checkout.go`, `internal/git/checkout_test.go`
- Modify: `internal/git/repository.go`

**Interfaces:**

```go
package git
type BranchSource string
const (Local BranchSource = "local"; Remote BranchSource = "remote"; Default BranchSource = "default")
type CheckoutStep struct { Repository Repository; Branch, StartPoint string; Source BranchSource; Create bool }
func PlanCheckout(context.Context, Runner, []Repository, string, bool) ([]CheckoutStep, error)
func ExecuteCheckout(context.Context, Runner, []CheckoutStep) error
```

- [ ] **Step 1: Write failing checkout tests**

Use a recording runner for local branch, remote tracking branch, absent branch from `origin/HEAD`, and fallback `origin/main`. Add tests for dirty, detached, uninitialized, fetch failure, and one branch-resolution failure. Assert no `switch` command is recorded for any failure and dry-run records no `fetch` or `switch` command.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/git -run 'Test(PlanCheckout|ExecuteCheckout)' -count=1`

Expected: FAIL because checkout APIs are absent.

- [ ] **Step 3: Implement preflight then mutation**

For non-dry-run, inspect every configured repository, reject dirty/detached/uninitialized, fetch each `origin`, and resolve all branches before calling `ExecuteCheckout`. Use existing local branch, then `switch --track --create <branch> origin/<branch>`, otherwise `switch --create <branch> <origin-default-or-origin/main>`. Execute only after every plan entry exists; format result summary from `CheckoutStep`.

- [ ] **Step 4: Run package and local-Git integration tests**

Run: `gofmt -w internal/git && go test ./internal/git -count=1`

Expected: PASS, including the no-switch-on-preflight-failure case.

- [ ] **Step 5: Manager review and focused commit**

Review all-or-nothing boundaries and dry-run immutability. Commit:

```sh
git add internal/git
git commit -m "feat(repo): plan safe coordinated checkout"
```

### Task 5: Check dispatcher and managed hooks

**Files:**
- Create: `internal/checks/runner.go`, `internal/checks/runner_test.go`, `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go`

**Interfaces:**

```go
package checks
type Executor interface { Run(context.Context, string, []string, string) error }
func Run(context.Context, Executor, config.Repository, []string, bool) error

package hooks
type InstallResult struct { Repository, Hook, Backup string }
func CommitMsgScript() []byte
func PreCommitScript(repositoryID string) []byte
func Install(workspace string, repositories []config.Repository, now func() time.Time) ([]InstallResult, error)
```

- [ ] **Step 1: Write failing check and hook tests**

Assert command checks run their exact `argv` from the repository directory; SQL formatting invokes `[pg_format -i <file>]` only for changed, non-ignored files matching `**/*.sql`; formatter failures stop dispatch. For hooks, assert `commit-msg` contains upward workspace discovery plus `validate-message "$1"`, configured repositories receive `pre-commit`, root/devops do not, existing unmanaged hooks rename to `<hook>.smt-backup.<UTC timestamp>`, generated hooks reinstall byte-identically, and missing executable `bin/smt` gives build/reinstall instructions.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/checks ./internal/hooks -count=1`

Expected: FAIL because both packages are absent.

- [ ] **Step 3: Implement the two isolated runners**

Use only supplied argument arrays. The hook POSIX template starts from `git rev-parse --show-toplevel`, walks parent directories until both `smt.yaml` and executable `bin/smt` exist, and exits nonzero with recovery instructions otherwise. Mark generated files with an SMT sentinel; only sentinel-bearing files are replaceable without backup.

- [ ] **Step 4: Run focused tests**

Run: `gofmt -w internal/checks internal/hooks && go test ./internal/checks ./internal/hooks -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review no shell execution, backup policy, and formatter behavior. Commit:

```sh
git add internal/checks internal/hooks
git commit -m "feat(repo): add checks and managed hooks"
```

### Task 6: Provider-neutral review contract and GitLab adapter

**Files:**
- Create: `internal/review/provider.go`, `internal/review/gitlab.go`, `internal/review/gitlab_test.go`

**Interfaces:**

```go
package review
type Request struct { Project, SourceBranch, TargetBranch, Title, Description string; Draft bool }
type Result struct { Number int; URL string }
type Provider interface { FindOpen(context.Context, Request) (*Result, error); Create(context.Context, Request) (Result, error) }
type Factory interface { For(config.Repository) (Provider, error) }
func NewGitLab(token, baseURL string, timeout time.Duration) (Provider, error)
```

- [ ] **Step 1: Write failing GitLab adapter tests**

Use `httptest.Server` to assert list-open-MR requests use the configured base URL, encoded configured project, `state=opened`, and source-branch filtering; assert create includes source branch, target branch, title, description, and draft. Test a non-2xx response reports `gitlab`, project, and HTTP status while redacting a sentinel token and Authorization header.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/review -run 'TestGitLab' -count=1`

Expected: FAIL because the review package is absent.

- [ ] **Step 3: Implement the normalized interface and GitLab client**

Construct the official client with the token and configured base URL, wrap each request in `context.WithTimeout`, list only opened merge requests then select the source branch, and create only after `FindOpen` returns nil. Return normalized number/URL. Do not infer a project from remotes and never include the token or raw client settings in errors.

- [ ] **Step 4: Run tests**

Run: `gofmt -w internal/review && go test ./internal/review -run 'TestGitLab' -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review context timeouts, provider-path errors, and redaction. Commit:

```sh
git add internal/review go.mod go.sum
git commit -m "feat(repo): add GitLab review provider"
```

### Task 7: GitHub adapter, Enterprise endpoints, and mixed-provider factory

**Files:**
- Create: `internal/review/github.go`, `internal/review/github_test.go`, `internal/review/factory.go`, `internal/review/factory_test.go`

**Interfaces:**

```go
package review
func NewGitHub(token, enterpriseBaseURL, enterpriseUploadURL string, timeout time.Duration) (Provider, error)
func ParseGitHubProject(project string) (owner, repository string, err error)
func NewFactory(cfg config.Config, gitlabToken, githubToken string, timeout time.Duration) Factory
```

- [ ] **Step 1: Write failing GitHub and factory tests**

Use `httptest.Server` to assert `PullRequests.List` uses open state, filters matching head branch, and `PullRequests.Create` sends title, head, base, body, and draft. Test malformed `project` values (`owner`, `owner/repo/extra`, `/repo`, `owner/`) fail before any request. Test both Enterprise URLs must be present together, endpoint configuration reaches the test server, GitLab/GitHub factory selection works in one workspace, and errors redact the token.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/review -run 'Test(GitHub|Factory|ParseGitHubProject)' -count=1`

Expected: FAIL because GitHub adapter functions are absent.

- [ ] **Step 3: Implement GitHub-only details**

Parse owner/repository once in the constructor. Create the official client with token authentication and bounded timeout; if either Enterprise URL is set, require both and configure them together. List open PRs and select the requested `Head`; create `NewPullRequest{Title, Head, Base, Body, Draft}` only after no open match. Git never creates refs in this adapter.

- [ ] **Step 4: Run tests**

Run: `gofmt -w internal/review && go test ./internal/review -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review project parsing, Enterprise pairing, no ref creation, and secret redaction. Commit:

```sh
git add internal/review go.mod go.sum
git commit -m "feat(repo): add GitHub review provider"
```

### Task 8: Submit service—discovery, preflight, commit ordering, and recovery

**Files:**
- Create: `internal/submit/service.go`, `internal/submit/service_test.go`

**Interfaces:**

```go
package submit
type Options struct { Target, Message, Description string; Draft, DryRun bool }
type Action struct { Kind, Repository, Detail, Recovery string }
type Summary struct { Actions []Action }
type Service struct { Config config.Config; Policy commit.Policy; Git git.Runner; Checks checks.Executor; Reviews review.Factory; Output io.Writer }
func (Service) Submit(context.Context, Options) (Summary, error)
```

- [ ] **Step 1: Write failing orchestration tests**

With recording fakes, assert: invalid message causes no Git/check/provider calls; only changed repositories are selected; all changed checks finish before the first `add`/`commit`; SQL formatting re-evaluates change state; changed submodules commit before root; root is skipped without a staged gitlink change; every commit uses the exact full validated message; only newly committed repositories push and request review; existing open review skips create; dry-run makes no formatter/check/Git-write/push/API calls; missing token for each changed provider errors before mutation; remote push or review failure stops, contains completed commits/pushes/reviews plus `smt submit --target <target> --message <message>`, and records no rollback command.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/submit -count=1`

Expected: FAIL because `Service.Submit` is absent.

- [ ] **Step 3: Implement the exact submit phases**

Implement phases in this order: validate message; discover non-ignored changed repositories; require changed-provider tokens and non-detached/non-target branches; report dry-run and return; run every check; stage/commit changed non-root repositories; re-inspect and stage/commit root only when its gitlink or root changes produce a staged result; push each new commit with `push --set-upstream origin <branch>`; find/create review; on remote failure return a `Summary` that has only completed actions and one safe rerun command. Default description identifies SMT, source branch, target branch, and commit SHA; an explicit description replaces it.

- [ ] **Step 4: Run focused tests and all internal tests**

Run: `gofmt -w internal/submit && go test ./internal/submit -count=1 && go test ./internal/... -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review preflight boundary, root-last ordering, dry-run immutability, token access, and recovery-only failure handling. Commit:

```sh
git add internal/submit
git commit -m "feat(repo): submit ordered monorepo changes"
```

### Task 9: Temporary-local-Git end-to-end contract tests

**Files:**
- Create: `internal/testgit/testgit.go`, `internal/git/checkout_integration_test.go`, `internal/submit/submit_integration_test.go`

**Interfaces:**

```go
package testgit
func Init(t *testing.T, root string) string
func Run(t *testing.T, dir string, args ...string) string
func SetRemoteHEAD(t *testing.T, bareRemote, branch string)
```

- [ ] **Step 1: Write failing local-repository tests**

Create root plus initialized submodule-shaped local repositories with bare `origin` remotes. Prove checkout sends exact argument arrays, creates tracking branches or default fallback only after all preflights pass, and leaves every HEAD unchanged on an injected preflight failure. Prove submit dry-run leaves HEADs, index, worktree files, hooks, remote refs, and review fake-call count unchanged.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/git ./internal/submit -run 'Integration' -count=1`

Expected: FAIL because `testgit` helpers are absent.

- [ ] **Step 3: Implement test-only helpers and dependency injection seams**

Keep `internal/testgit` test support scoped to local-repository construction; invoke the real `git` binary only with argument arrays, and use `t.Setenv` for harmless fake provider endpoints only—not tokens. Add only the public seams already defined in Tasks 3, 4, and 8; do not add production-only abstractions solely for tests.

- [ ] **Step 4: Run integration and full Go suite**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Manager review and focused commit**

Review that test fixtures contain no credentials and that dry-run proof covers every mutating category. Commit:

```sh
git add internal/git internal/submit
git commit -m "test(repo): cover local Git checkout and submit plans"
```

### Task 10: CLI wiring and command-level tests

**Files:**
- Create: `cmd/smt/main.go`, `cmd/smt/main_test.go`, `cmd/smt/checkout.go`, `cmd/smt/hooks.go`, `cmd/smt/validate.go`, `cmd/smt/submit.go`

**Interfaces:**

```go
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int
// commands: checkout <branch> [--dry-run]; hooks install;
// validate-message <file>; validate-range --from <sha> --to <sha>;
// submit --target <branch> --message <message> [--description <text>] [--draft] [--dry-run]
```

- [ ] **Step 1: Write failing command tests**

Test usage errors for every command, success output for checkout plan/hooks summary/validation, `validate-message` reading the complete file, `validate-range` listing every invalid SHA and reason before nonzero exit, submit flag propagation, and provider/Git errors mapped to concise stderr without raw token values. Use injected services/fakes; no command test uses real network.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./cmd/smt -count=1`

Expected: FAIL because `run` and command files are absent.

- [ ] **Step 3: Implement standard-library flag parsing and wiring**

Use `flag.FlagSet` per command, require exact positional arguments, load `smt.yaml` from the current workspace, construct services with bounded contexts, print summaries to stdout, and return nonzero for validation/operational errors. `validate-range` reads `git log --format=%H%x00%B%x00 from..to` through the Git package and aggregates all invalid messages.

- [ ] **Step 4: Build and run command tests**

Run: `gofmt -w cmd/smt && go test ./cmd/smt -count=1 && go build -o bin/smt ./cmd/smt`

Expected: PASS and `bin/smt` exists.

- [ ] **Step 5: Manager review and focused commit**

Review only command construction, exit codes, and token-free output. Commit:

```sh
git add cmd/smt
git commit -m "feat(repo): add SMT command-line interface"
```

### Task 11: CI range-validation workflows, release pin, and documentation handoff

**Files:**
- Create: `.gitlab-ci.yml`, `.github/workflows/commit-validate.yml`
- Create: `internal/ci/contract_test.go`
- Modify: `README.md`, `docs/00-project/SMT - Implementation Spec.md`, `prompts/smt-build.md`

**Interfaces:**

```yaml
# GitLab uses $CI_MERGE_REQUEST_DIFF_BASE_SHA and $CI_COMMIT_SHA.
# GitHub uses github.event.pull_request.base.sha and github.event.pull_request.head.sha.
# Both install github.com/parmcoder/smt/cmd/smt@v0.1.0 after the v0.1.0 tag exists.
```

- [ ] **Step 1: Write workflow/static contract tests**

In `internal/ci/contract_test.go`, add `TestWorkflowContract` that reads both workflow files and asserts the exact pinned install string, GitLab merge-request rule with both SHA variables, GitHub `fetch-depth: 0`, and `smt validate-range` arguments. Add `TestREADMEContract` that requires headings or phrases for installation, configuration, prerequisites, hooks/recovery, CI pinning, and partial-failure recovery.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./... -run 'Test(Workflow|README)Contract' -count=1`

Expected: FAIL because workflow files and contract tests are absent.

- [ ] **Step 3: Implement CI and route documentation work correctly**

`backend_worker` writes only the CI behavior and its contract tests when explicitly assigned. After acceptance, `doc_writer` updates README/spec/prompt with the exact tagged-module install, command examples, token prerequisites without values, hook backup/reinstall instructions, and non-destructive recovery command. The documentation worker must not change CLI or CI behavior.

- [ ] **Step 4: Validate workflows, docs, and full Go suite**

Run: `go test ./... -count=1 && ruby -e 'require "yaml"; YAML.load_file(".gitlab-ci.yml"); YAML.load_file(".github/workflows/commit-validate.yml")'`

Expected: PASS. If Ruby's YAML parser is unavailable, record that as unverified and validate the Go contract tests plus YAML parser available in the repository toolchain.

- [ ] **Step 5: Manager final review and release commit**

Review CI pinning, docs alignment, and no leaked tokens. Commit only assigned paths:

```sh
git add .gitlab-ci.yml .github/workflows/commit-validate.yml README.md docs prompts
git commit -m "ci(repo): validate commit ranges with SMT"
```

### Task 12: Final acceptance and handoff

**Files:**
- Verify: all paths from Tasks 1–11; no new behavior files

- [ ] **Step 1: Run the full acceptance command set**

Run:

```sh
go test ./... -count=1
go build -o bin/smt ./cmd/smt
./bin/smt validate-message /path/to/valid-commit-message
git diff --check
git status --short
```

Expected: tests/build pass; validation accepts a known `feat(repo): ...` full message; no whitespace errors; status names only intended work.

- [ ] **Step 2: Manager performs final integration review**

Verify the delivery checklist against the implementation specification: all five commands, config validation, strict message policy, checkout all-or-nothing, hook backup/idempotence, check dispatch, preflight-before-commit, submodule-before-root ordering, mixed review providers, error redaction, partial remote recovery, dry-run immutability, and CI range validation.

- [ ] **Step 3: Collect the required handoff report**

Report changed paths; every command and result; release/tag and remote operations left unperformed; assumptions; unresolved risks; and unverified behavior. Do not stage, revert, reset, or otherwise change unrelated user work.

## Plan self-review

- **Specification coverage:** Tasks 1–2 cover configuration and full-message policy; 3–5 cover Git state, checkout, checks, and hooks; 6–7 cover both review providers and redaction; 8–9 cover submit ordering, recovery, and local-Git proof; 10 covers CLI/CI command integration; 11 covers tagged CI and documentation; 12 is the acceptance gate.
- **No placeholders:** every task names concrete paths, interfaces, failing test cases, passing commands, and commit scope. The active worker contract authorizes every Go path in the plan.
- **Type consistency:** `config.Repository` feeds checks/review/submit; `git.Runner` feeds Git and submit; `commit.Policy` feeds submit/CLI; `review.Request` and `review.Provider` are the sole provider boundary.

Plan complete and saved to `docs/superpowers/plans/2026-07-16-smt-cli-implementation.md`. The approved role contract uses the manager-reviewed, serialized worker loop; execute it with one fresh `backend_worker` assignment per task and `doc_writer` alignment after accepted behavior.
