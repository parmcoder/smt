---
type: implementation-plan
status: approved-planned
owner: platform
tags:
  - smt
  - flutter
  - mobile
  - blueprint
created: 2026-08-09
updated: 2026-08-09
---
# Flutter Mobile Component Plan

## Summary

Deliver the approved optional Flutter Mobile component without changing the
version-1 configuration format or the scaffold-only safety boundary. This plan
records ordered delivery work; it does not claim Mobile is implemented today.
See [[../specs/2026-08-09-flutter-mobile-component-design|Flutter Mobile Component Design]] and [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]].

## Ordered delivery

1. `smt-3r2.1` — define the canonical contract, design, and plan. This blocks
   `smt-3r2.2`.
2. `smt-3r2.2` — extend the version-1 configuration and `smt new` blueprint
   prompt. The literal prompt follows Web: `Include Flutter mobile
   application? [Y/n]`; Enter includes Mobile and explicit no excludes it.
   Selected order is repo, web, mobile, api, database, infra. Existing
   version-1 blueprints without Mobile remain valid and have no Mobile output.
3. `smt-3r2.3` — implement atomic apply validation and the Mobile component
   shell. Accept only `workspace.stack.mobile: flutter` with repository
   `id: mobile`, `path: mobile-app`, `component: mobile`,
   `technology: flutter`, and `scope: mobile`; reject any unsupported stack or
   mismatched metadata before mutation. Scope is Android and iOS only.
4. `smt-3r2.4` — align final documentation and run release verification after
   accepted behavior.
5. `smt-3r2.5` — human-owned end-to-end review of the default and opt-out
   blueprint lifecycle. It is later work, not current runtime proof.

## Implementation constraints

The generated shell in `smt-3r2.3` includes the independent initialized local
bootstrap submodule, `mobile_worker` manifest, Flutter-oriented README and
ignore rules, and `.tool-versions` containing literal `flutter 3.44.9`.
`smt apply` validates first and remains atomic/all-or-nothing: prerequisite,
staging, Beads, or publish failure leaves no partial destination. Existing
all-or-nothing behavior remains, including no remote rollback after a later
submit failure.

The feature is strictly scaffold-only. Do not invoke `flutter create`,
`flutter --version`, or another Flutter CLI; require a Flutter SDK/executable;
install dependencies; access the network; or promise generated Flutter source.

## Verification contract

Focused tests must cover default inclusion, explicit opt-out, invalid-answer
retry, EOF/decline no-write, exact YAML/repository/scopes/order, invalid
stack/metadata rejection before mutation, version-1 compatibility, atomic
preflight and stage/publish cleanup, generated artifacts, and test execution
without Flutter. Human E2E belongs only to `smt-3r2.5`.

## Related

- [[../specs/2026-08-09-flutter-mobile-component-design|Flutter Mobile Component Design]]
- [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]]
- [[../../00-project/SMT - Product Concept|SMT — Product Concept]]
- [[../../00-project/SMT - Agent Team|SMT — Agent Team]]
