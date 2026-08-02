---
type: delivery-plan
status: approved
implementation_status: paused
owner: platform
tags:
  - smt
  - bubbletea
  - go-git
  - beads
  - human-review
created: 2026-08-01
updated: 2026-08-02
---
# SMT — Agentic Development Program

## Outcome

Deliver the next SMT release defined in [[../../00-project/SMT - Implementation
Spec]]: a Bubble Tea human workflow, go-git runtime, prerequisite-safe
scaffolding, Beads-backed delivery, an Obsidian collaboration workspace, and a
mandatory human E2E review gate.

## Documentation research record

Context7 resolution on 2026-08-01 identified Bubble Tea at
`/websites/pkg_go_dev_github_com_charmbracelet_bubbletea`; its documentation
confirms the `Init`/`Update`/`View` model, program run loop, and alternate
screen support. Context7 resolved go-git at `/go-git/go-git`; its documentation
describes pure-Go repository initialization and remote operations. The release
pins Bubble Tea v2.0.8 and go-git v5.19.2 as product decisions; implementation
must verify selected module APIs when code is added.

## Beads feature sequence

The live Beads graph is the durable delivery record. `smt-4rb.1` is
implementation-reviewed but remains **in progress** while its human E2E review
`smt-4rb.1.1` is open; it is not accepted or closed.

1. **`smt-4rb.1` — Canonicalize next-release contract.** Update the canonical
   implementation specification and save the approved execution plan before
   production-code changes. Acceptance: the spec defines Bubble Tea, stable
   go-git, workflow config v1, prerequisites, generated docs, Beads review
   gates, worktree removal, and verification boundaries. Human gate:
   `smt-4rb.1.1` reviews the modified canonical files and records pass
   evidence or a complete failure report.
2. **`smt-4rb.2` — Migrate SMT runtime to go-git.** Replace compiled SMT
   system-Git execution with go-git v5.19.2 and remove synchronized worktree
   behavior. Acceptance: repository inspection, initialization, commits,
   submodule gitlinks, and child-before-root push use go-git; the worktree
   command and runtime Git check are removed; safety and redaction tests pass.
3. **`smt-4rb.3` — Add workflow config and prerequisite-first scaffold.** Add
   optional workflow config plus prerequisite detection and atomic generated
   collaboration-workspace scaffolding. Acceptance: new init detects
   `codex`/`asdf`/`bd`, plugins, and runtimes without installing them; it
   generates tool versions, Beads integration, agents, prompts,
   docs/templates/Base, and publishes only on success.
4. **`smt-4rb.4` — Implement Beads human-review domain.** Implement
   JSON-backed Beads work plus feature-review-bug-retest state transitions.
   Acceptance: feature handoff queues a human review; pass unblocks the
   feature; fail requires and creates a linked bug; a fixed bug requeues the
   same review; release readiness blocks on open reviews and bugs.
5. **`smt-4rb.5` — Build Bubble Tea full-screen application.** Implement
   Setup, Work, Human Review, and Workspace Health over accepted adapters.
   Acceptance: TTY default opens the full-screen app; explicit headless
   commands stay deterministic; asynchronous operations, cancellation, resize,
   `NO_COLOR`, and redirected-output behavior are tested.
6. **`smt-4rb.6` — Document results, quick starts, and final acceptance.**
   Align repository and generated documentation with verified behavior and
   complete release-level verification. Acceptance: README, command recipes,
   team/spec docs, prompts, generated quick starts, and review templates match
   verified commands; full test/build/doc checks pass; release-level human E2E
   review is queued.

`work_manager` serializes engineering assignments in `smt-4rb.1` through
`smt-4rb.6` order. Beads `tracks` links intentionally do not block unrelated
ready work. A queued human review blocks its own feature acceptance, and every
open human review or review-originated bug blocks release readiness.

## Paused implementation handoff — 2026-08-02

Implementation stopped in `/private/tmp/smt-agentic-development` on branch
`codex/agentic-development-program` at base revision `46aabfb`, without a
commit or push. The automated portion of `smt-4rb.5.1` has completed the Setup
screen, the Work queue flow, and Human Review read-only rendering plus the Pass
open/focus path. These slices are pending human acceptance; they do not close
`smt-4rb.5` or satisfy a human-owned review.

The exact remaining `smt-4rb.5.1` slices, in resume order, are:

1. Finish Pass evidence input ownership, validation, typed `HumanPass`
   completion/recovery/cancellation. Reviewer ownership already exists.
2. Human Review Fail input/form flow and linked-bug creation boundary, with
   renderer-safety and width tests.
3. Human Review Requeue/retest flow after a resolved bug, with renderer-safety
   and width tests.
4. Workspace Health aggregation for configuration, repository status,
   prerequisites, and release blockers.
5. Bubbles spinner and asynchronous-operation recovery presentation.
6. Injectable TTY/application routing plus the headless safe-DTO, usage, and
   redaction audit.

After those slices, `smt-4rb.6` remains responsible for verified repository
documentation, quick starts, generated-artifact guidance, final checks, and
queueing the release-level human E2E review. Do not claim final acceptance
until its evidence is complete.

Open human-review gates at this handoff are `smt-4rb.1.1` (canonical
contract), `smt-s2w` (go-git runtime), `smt-1by` (prerequisite-gated atomic
init), and `smt-4rb.4.2` (Beads review lifecycle). They remain human-owned;
agents must not close them. No human review for `smt-4rb.5` has been queued
yet.

Resume with focused tests for the affected TUI package, then run:

```sh
GOCACHE=/private/tmp/smt-gocache go test ./... -count=1
GOCACHE=/private/tmp/smt-gocache go vet ./...
GOCACHE=/private/tmp/smt-gocache go build -o /private/tmp/smt-agentic-development-check ./cmd/smt
git diff --check
```

All four commands passed in the final resource-aware verification on
2026-08-02.

Before handing off, perform the pending TTY/headless audit and record its
actual commands and evidence in the related Beads feature/review. Do not
commit, push, or sync Beads without new explicit authority.

## Verification

Run focused package tests while implementing each feature, then run:

```sh
GOCACHE=/private/tmp/smt-gocache go test ./...
git diff --check
rg -n 'worktree add|linked worktree|ExecRunner|exec\.Command.*git' cmd internal
rg -n 'standard-library CLI and Git execution|worktree add|linked worktree|Git executable' README.md docs prompts AGENTS.md CLAUDE.md
```

The final integration lane additionally runs a new workspace through the
interactive TTY path and explicit non-TTY guidance with system Git absent from
SMT's runtime `PATH`. It validates the two plugin selectors, conditional
`.tool-versions` content, atomic staging cleanup, Beads pass/fail/bug/retest
links, human evidence capture, and release gating.

## Scope boundaries

SMT displays prerequisite guidance but does not install global software.
Existing workspaces are not migrated. Provider PR/MR creation, deployment, and
automatic execution of human E2E tests remain out of scope.
