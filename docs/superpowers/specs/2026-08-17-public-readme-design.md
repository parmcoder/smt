---
type: documentation-design
status: active
owner: platform
tags:
  - smt
  - readme
  - onboarding
created: 2026-08-17
updated: 2026-08-17
---
# Public README Design

## Audience and truth boundary

The root README serves new visitors, potential users, and contributors who
need a source-first path from checkout to a reviewed local workspace. It must
separate current implemented commands from the planned module/starter
restructure and must not imply package-manager installation or runtime
features that are not delivered.

## Structure

Use this order: centered hero and honest badges; development-status callout;
why SMT; available today; source-first getting started; workflow diagram;
roadmap; safety principles; documentation links; contributing; license.
Links target canonical repository docs, with GitHub-compatible percent-encoded
paths where needed. The only Mermaid is the small onboarding workflow.

Badges are limited to In development, Go 1.26.5, and MIT. Do not add a CI
badge because the repository has release-on-tag automation rather than a
general build-status contract.

## Acceptance

README commands and flags must match source or canonical recipes. Fresh-clone
onboarding must include Git, Go, Task, and Beads `bd`; Lefthook is optional
until hook installation. The README stays concise, removes detailed hook and
release runbooks, links their canonical sources, contains no placeholders, and
passes link, Mermaid, and `git diff --check` validation.
