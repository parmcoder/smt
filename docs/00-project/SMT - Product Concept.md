---
type: product-concept
status: active
owner: platform
tags:
  - smt
  - product
  - git
  - developer-experience
created: 2026-07-16
updated: 2026-08-09
---
# SMT — Product Concept

SMT gives Sanovy developers and Codex agents one inspectable tool for a root
Git repository with independent submodules. It creates a reviewable blueprint
before applying a local platform workspace, then safely coordinates its normal
Git lifecycle without hiding state.

Its local workflow is:

```mermaid
flowchart LR
    A[smt new] --> B[Inspect and edit smt.yaml]
    B --> C[smt apply PATH]
    C --> D[Configure remote URLs]
    D --> E[status or doctor]
    E --> F[Run named check profile]
    F --> G[Push children then root]
    F --> H[Create synchronized worktree]
    F --> I[Validate CI contracts]
    I --> J[Plan guarded literal bump]
    J --> K[Apply only with explicit approval]
```

Safety is the product: argument-array execution, an explicit blueprint review
before workspace creation, complete preflight before push/worktree side
effects, child-first pushes, root-first worktree creation, path containment,
harmless OS metadata filtering, and no credential persistence. Cobra groups
the discoverable command tree into Getting Started, Workspace, Review
Workflow, and Developer Tools; help and generated shell completion work
without workspace configuration. The current CLI covers blueprint creation and
application, inspection, checks, contracts, CI audits, guarded contract bumps,
configured pushes, linked worktrees, local work/review workflow, and release
readiness. The release path is deliberately split: `release:build` makes four
local archives and checksums, while a clean `release:tag` creates and pushes an
annotated version tag. GitHub Actions now publishes a GitHub Release from that
tag with the four archives and checksum file.

Direct provider-native release CLI orchestration does not exist, and GitLab
release automation does not exist. Remote repository creation, external-clone
submodule URL synchronization, submit orchestration, cloud or database actions,
deployment, rollback, and credential storage remain outside the current product
boundary.

## Related

- [[SMT - Implementation Spec]] — canonical behavior and boundaries.
- [[../10-development/SMT - Command Recipes]] — runnable examples.
- [[SMT - Agent Team]] — ownership and documentation workflow.
- [[../../AGENTS|Repository operating agreement]].
