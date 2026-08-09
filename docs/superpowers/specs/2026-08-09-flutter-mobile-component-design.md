---
type: component-design
status: approved-planned
owner: platform
tags:
  - smt
  - flutter
  - mobile
  - blueprint
  - android
  - ios
created: 2026-08-09
updated: 2026-08-09
---
# Flutter Mobile Component Design

## Summary

The approved Mobile component extends SMT version-1 blueprints with an optional
Flutter repository for Android and iOS. It is a Git-ready scaffold only: no
Flutter SDK execution, dependency installation, network access, or generated
application source is part of this design. The canonical contract is
[[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]].

## Selection and blueprint contract

`smt new` asks `Include Flutter mobile application? [Y/n]` immediately after
the Web selection. Enter selects Mobile; an explicit no excludes it. The
selected component and repository order is repo, web, mobile, api, database,
infra.

Mobile keeps configuration at `version: 1`. Existing version-1 configurations
without Mobile remain valid and apply without Mobile output. A selected Mobile
component has exactly one supported stack and repository mapping:

```yaml
workspace:
  stack:
    mobile: flutter
repositories:
  - id: mobile
    path: mobile-app
    component: mobile
    technology: flutter
    scope: mobile
```

Only Android and iOS are in version-1 scope. Unsupported Mobile stacks, or any
mismatched Mobile metadata, fail validation before mutation.

```mermaid
flowchart LR
    A["smt new"] --> B["Web selection"]
    B --> C["Mobile prompt: Enter = Yes"]
    C --> D["Inspect version-1 blueprint"]
    D --> E["smt apply preflight and validation"]
    E --> F["Atomic Git-ready workspace"]
```

## Scaffold boundary and safety

Applying a selected Mobile blueprint creates an independent initialized local
bootstrap submodule at `mobile-app`, a `mobile_worker` manifest,
Flutter-oriented README and ignore rules, and `.tool-versions` with the
literal `flutter 3.44.9` pin. It must not run `flutter create`,
`flutter --version`, or any Flutter SDK CLI; require Flutter; install
dependencies; access the network; or promise Flutter source generation.

`smt apply` validates before mutation and remains atomic/all-or-nothing. A
prerequisite, staging, Beads, or publish failure leaves no partial destination.
Existing recovery semantics remain: do not remote-roll back after a later
submit failure.

## Delivery and verification

This documentation task, `smt-3r2.1`, blocks the configuration/blueprint work
in `smt-3r2.2`; it precedes atomic apply work in `smt-3r2.3`, final
documentation/release verification in `smt-3r2.4`, and human-owned E2E in
`smt-3r2.5`. This is approved planned behavior, not a claim of current Go
support.

Focused verification covers inclusion by default, opt-out, invalid-answer
retry, EOF/decline no-write, exact YAML/mapping/scopes/order, invalid stack and
metadata rejection before mutation, version-1 compatibility, atomic preflight
and stage/publish cleanup, generated artifacts, and tests without Flutter.
Human runtime E2E is deferred to `smt-3r2.5` and is not claimed complete here.

## Related

- [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]]
- [[../../00-project/SMT - Product Concept|SMT — Product Concept]]
- [[../../00-project/SMT - Agent Team|SMT — Agent Team]]
- [[../plans/2026-08-09-flutter-mobile-component|Flutter Mobile Component Plan]]
