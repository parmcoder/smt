---
type: component-design
status: active
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
the Web selection. Enter selects Mobile; an explicit no excludes it. When
Mobile is selected, the component and repository order is repo, web, mobile,
api, database, infra; an opt-out omits Mobile.

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
literal `flutter 3.44.9` pin. It does not invoke `flutter create`,
`flutter --version`, or any Flutter SDK CLI. It does not require Flutter,
install dependencies, access the network, produce Flutter source, sign an app,
or publish an app.

`smt apply` validates before mutation and remains atomic/all-or-nothing. A
prerequisite, staging, Beads, or publish failure leaves no partial destination.
Existing recovery semantics remain: do not remote-roll back after a later
submit failure.

## Delivery and verification

The contract, configuration/blueprint work, atomic apply, and documentation
alignment are delivered through `smt-3r2.4`. Human-owned E2E remains
`smt-3r2.5`; this document does not claim that runtime review is complete.

Focused verification covers inclusion by default, opt-out, invalid-answer
retry, EOF/decline no-write, exact YAML/mapping/scopes/order, invalid stack and
metadata rejection before mutation, version-1 compatibility, atomic preflight
and stage/publish cleanup, generated artifacts, and tests without Flutter.
Human runtime E2E is deferred to `smt-3r2.5` and is not claimed complete here.

## Human E2E review handoff

Create a default blueprint by pressing Enter at the Mobile prompt and an
opt-out blueprint by answering no. Apply each only to a new destination. For
the default case, verify the ordered Mobile configuration plus `mobile-app`,
`agents/mobile_worker.toml`, the Mobile README and ignore rules, and the
`flutter 3.44.9` pin. The review must not expect Flutter source generation or
use of Flutter, its SDK, dependencies, network, signing, or store publication.
At one additional fresh destination, exercise one safe prerequisite, staging,
Beads, or publish failure and verify that no partial destination remains.

## Related

- [[../../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]]
- [[../../00-project/SMT - Product Concept|SMT — Product Concept]]
- [[../../00-project/SMT - Agent Team|SMT — Agent Team]]
- [[../plans/2026-08-09-flutter-mobile-component|Flutter Mobile Component Plan]]
