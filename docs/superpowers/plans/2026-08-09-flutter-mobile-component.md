---
type: implementation-plan
status: active
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

The optional Flutter Mobile component is delivered without changing the
version-1 configuration format or the scaffold-only safety boundary. This plan
records the completed delivery sequence and the remaining human E2E review.
See [[../specs/2026-08-09-flutter-mobile-component-design|Flutter Mobile Component Design]] and [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]].

## Ordered delivery

1. `smt-3r2.1` — defined the canonical contract, design, and plan.
2. `smt-3r2.2` — extended the version-1 configuration and `smt new` blueprint
   prompt. The literal prompt follows Web: `Include Flutter mobile
   application? [Y/n]`; Enter includes Mobile and explicit no excludes it.
   When Mobile is selected, order is repo, web, mobile, api, database, infra;
   an explicit opt-out omits Mobile. Existing version-1 blueprints without
   Mobile remain valid and have no Mobile output.
3. `smt-3r2.3` — implemented atomic apply validation and the Mobile component
   shell. Accept only `workspace.stack.mobile: flutter` with repository
   `id: mobile`, `path: mobile-app`, `component: mobile`,
   `technology: flutter`, and `scope: mobile`; reject any unsupported stack or
   mismatched metadata before mutation. Scope is Android and iOS only.
4. `smt-3r2.4` — aligned final documentation and ran release verification
   after accepted behavior.
5. `smt-3r2.5` — human-owned end-to-end review of the default and opt-out
   blueprint lifecycle. It is later work, not current runtime proof.

## Delivered constraints

The generated shell in `smt-3r2.3` includes the independent initialized local
bootstrap submodule, `mobile_worker` manifest, Flutter-oriented README and
ignore rules, and `.tool-versions` containing literal `flutter 3.44.9`.
`smt apply` validates first and remains atomic/all-or-nothing: prerequisite,
staging, Beads, or publish failure leaves no partial destination. Existing
all-or-nothing behavior remains, including no remote rollback after a later
submit failure.

The feature is strictly scaffold-only. It does not invoke `flutter create`,
`flutter --version`, or another Flutter CLI. It does not require a Flutter
SDK/executable, install dependencies, access the network, produce generated
Flutter source, sign an app, or publish an app.

## Verification contract

Focused tests cover default inclusion, explicit opt-out, invalid-answer
retry, EOF/decline no-write, exact YAML/repository/scopes/order, invalid
stack/metadata rejection before mutation, version-1 compatibility, atomic
preflight and stage/publish cleanup, generated artifacts, and test execution
without Flutter. Human E2E belongs only to `smt-3r2.5`.

## Human E2E review handoff

`smt-3r2.5` should create default-included and explicit-no blueprints, apply
both to fresh destinations, and capture the resulting YAML and artifacts. For
the default case, review the Mobile ordering, Git-ready `mobile-app`,
`mobile_worker` manifest, Flutter README and ignore rules, and `.tool-versions`
pin. The reviewer needs no Flutter installation and must not treat absent app
source, dependencies, network use, signing, or publication as failures. At one
additional fresh destination, exercise one safe prerequisite, staging, Beads,
or publish failure and verify that no partial destination remains.

## Related

- [[../specs/2026-08-09-flutter-mobile-component-design|Flutter Mobile Component Design]]
- [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]]
- [[../../00-project/SMT - Product Concept|SMT — Product Concept]]
- [[../../00-project/SMT - Agent Team|SMT — Agent Team]]
