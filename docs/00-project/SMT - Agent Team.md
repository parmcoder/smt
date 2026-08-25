---
type: team-contract
status: active
owner: platform
tags:
  - agents
  - go
  - documentation
  - smt
created: 2026-07-15
updated: 2026-08-22
---
# SMT — Agent Team

## Team shape

The active delivery team uses one Terra/high work manager and Luna worker
contracts for implementation, documentation, and root integration. Every
worker uses GPT-5.6 Luna with the Fast priority tier and extra-high reasoning.
The manager serializes bounded implementation, reviews the integrated result,
and loops only when blocking findings require remediation by the same worker.
This keeps architecture and final quality decisions on the stronger model
without making the manager a component implementer.

The manifests are stored in `agents/` for this checkout. A host integration
can copy or register them in its native agent directory if required.

| Agent | Model | Owns | Must not own |
| --- | --- | --- | --- |
| `work_manager` | `gpt-5.6-terra`, high | Serial delivery assignments, safety decisions, worker review loop, final acceptance | Go/Flutter/E2E implementation, delegation beyond the listed workers |
| `backend_worker` | `gpt-5.6-luna`, Fast, xhigh | Go production code and focused tests assigned by `work_manager` under `internal/` and `cmd/smt/` | Architecture decisions, docs, further delegation, non-manager assignments |
| `web_worker` | `gpt-5.6-luna`, Fast, xhigh | Next.js/TypeScript Web production code and focused tests assigned by `work_manager` | Architecture decisions, docs, further delegation, Go/Flutter assignments |
| `mobile_worker` | `gpt-5.6-luna`, Fast, xhigh | Only assigned Flutter/Dart production code and focused tests; explicit SDK/device lane reporting | Architecture decisions, docs, further delegation, Go assignments |
| `e2e_worker` | `gpt-5.6-luna`, Fast, xhigh | Root-attached `e2e/web` Playwright and `e2e/mobile` orchestration packages; contract smoke tests and explicit browser/device lane reporting | Domain CRUD/auth flows, component implementation, signing, cloud device farms, remote CI, further delegation |
| `doc_writer` | `gpt-5.6-luna`, Fast, xhigh | `docs/`, `prompts/`, durable decisions, handoffs, Mermaid | Go implementation and behavior changes |
| `integration_worker` | `gpt-5.6-luna`, Fast, xhigh | Root gitlinks and integration artifacts in prepared workspaces | Child implementation, Beads claims, agent launch, provider remotes |
| `backend_agent` | `gpt-5.6-terra`, high | Direct, explicitly requested architecture review outside a work-manager delivery | A concurrent work-manager delivery or `backend_worker` assignment |

`integration_worker` is a generated, host-neutral root-integration contract for
prepared workspaces. It owns only root gitlink and integration artifacts; it is
not a downstream implementation delegate. The active implementation topologies
are `work_manager -> backend_worker -> doc_writer` for Go,
`work_manager -> web_worker -> doc_writer` for Web,
`work_manager -> mobile_worker -> doc_writer` for Mobile, and
`work_manager -> e2e_worker -> doc_writer` for local E2E. The backend route
remains unchanged; `doc_writer` aligns docs only after accepted behavior.
The integration contract makes the root ownership boundary and feature-ID
commit rule explicit for whichever host performs the integration step.

`work_manager` and `backend_agent` use `$godex:godex-go-backend` for Go
architecture and review. `backend_worker` uses it for its assigned
implementation. The documentation worker uses `$codex-obsidian-writer` and
`$codex-obsidian-markdown`.

The E2E worker uses `$build-web-apps:frontend-testing-debugging` for
Playwright/browser work and `$flutter-add-integration-test` for Flutter's
native `integration_test` lane. It keeps Flutter integration-test files in the
Mobile app project so device execution remains native; the root-attached
`e2e/mobile` package owns commands, environment, fixtures, and reports.

## Execution flow

```mermaid
flowchart TD
    A[Read canonical spec and approved plan] --> B[work_manager resolves one bounded assignment]
    B --> C[component worker implements and runs task tests]
    C --> D[work_manager reviews integrated worker diff]
    D -->|blocking finding| C
    D -->|accepted behavior| E[doc_writer aligns high-level docs and prompts]
    E --> F[work_manager runs final integration validation]
```

## Accepted Mobile Apply boundary

The Mobile lane is implemented through the staged Flutter CLI workflow. The
`.3.5.1` contract owns the Flutter base-manifest policy: Apply creates the
Flutter CLI `pubspec.yaml`, `analysis_options.yaml`, and project baseline; the
`pubspec.lock` and pinned `flutter_lints 6.0.0` policy are produced and
verified later by `mobile_worker` after `asdf exec flutter pub get`. For
`.3.5.2`, after staging the root `.tool-versions` pin
`flutter 3.44.9-stable`, Mobile Apply runs and preserves this exact command in
the staged child:

```sh
asdf exec flutter --suppress-analytics create --empty --no-pub --platforms=android,ios --org=com.example.smt --project-name=smt_mobile --description="A provider-neutral SMT Flutter mobile starter." <staged-mobile-directory>
```

There are no static Android/iOS templates and no Go post-create writes for app
source, tests, or analysis. Apply does not run pub-get or package resolution;
missing Flutter is an atomic failure with `asdf install flutter 3.44.9-stable`
and `asdf current flutter` guidance. `mobile_worker` owns only assigned
Flutter/Dart code and focused tests, reports Android/iOS SDK or device
availability explicitly. The `.3.5.3` lane owns the generated app's Dart format,
Flutter analyze, unit/widget tests, and native integration-test contract; current
evidence passes format, analyze, and unit/widget tests. The host has no Android
SDK or supported Android/iOS target, so integration execution and debug builds
remain explicitly unverified.

## Local E2E worker boundary

The selected root `e2e` declaration is being expanded by `smt-4xf.14` into
separate `e2e/web` and `e2e/mobile` packages. Apply emits only the package
matching a selected Web or Mobile component; selecting E2E without either
target remains valid metadata-only. The first suite is contract smoke only:
stable Web navigation hooks and `/healthz`, optional API reachability, Mobile
launch, and the `mobile-home`/`api-status` keys. It contains no domain CRUD,
auth secrets, signing, cloud device farms, or implicit installs. Local tasks
delegate startup and shutdown to existing component tasks, retain failure
reports, and report missing browsers, SDKs, simulators, emulators, or devices
explicitly.

## Accepted Web Apply boundary

The Web `.3.2.1` initializer is implemented through the staged Next.js CLI
workflow. After the root `.tool-versions` pin `nodejs 24.18.0` is staged, Web
Apply runs this exact argument-array command and preserves the CLI-owned files:

```sh
asdf exec npx --yes create-next-app@16.2.9 <staged-web-directory> --typescript --eslint --app --empty --tailwind --use-pnpm --skip-install --disable-git --agents-md --import-alias=@/*
```

Apply merges the CLI `.gitignore`, publishes no package-manager lockfile, and
does not run `pnpm install` or resolve dependencies. A failed initializer leaves the
published destination absent and retains actionable recovery guidance:
`asdf install nodejs 24.18.0`, `asdf current nodejs`, and
`asdf exec npx --yes create-next-app@16.2.9 --help`. The CLI output is staged
before publication, so the failure is atomic.

The pinned `npx create-next-app` call is the sole Apply exception that may
access the npm registry. Non-Web and static Apply paths remain offline; Web
still performs no `pnpm install`, lockfile publication, or dependency
resolution.

When Web is selected, Apply also generates Web-specific `web_worker` routing
and the worker manifest. `web_worker` owns only assigned Next.js/TypeScript
production code and focused tests, uses the required
`build-web-apps:react-best-practices` and
`build-web-apps:frontend-testing-debugging` skills, does not delegate, and
reports unavailable Node, pnpm, browser, or platform lanes explicitly. The
later `.3.2.2/.3` work owns pnpm installation and lockfile creation, quality,
browser, and runtime verification; no such real-lane evidence is claimed by
`.3.2.1`.

## Beads ticket ownership

Agents create feature and task tickets directly with Beads before editing
code. Use `bd prime`, `bd create`, `bd show`, `bd update --claim`, `bd ready`,
`bd blocked`, and `bd close`; Beads is the source of truth for durable work
state. SMT no longer wraps ticket creation, review queues, ready-work listing,
or release readiness. `smt prepare` may still create its special internal
`Prepared workspace` task for repository lifecycle coordination.

## Delegation contract

Every `work_manager` implementation assignment must contain:

- owned paths and explicit non-owned paths;
- dependencies and decisions already resolved;
- acceptance criteria and the narrowest useful checks;
- a requirement to preserve unrelated user changes;
- the expected handoff format: changed paths, checks/results, assumptions,
  unresolved risks, and unverified behavior.

`work_manager` is the worker's sole implementation-assignment issuer. Each
worker implements only its assigned component code and focused tests, does not
spawn agents or silently change the design, and returns implementation/test
evidence to `work_manager`. Component workers report unavailable SDK, device,
Android, or iOS lanes explicitly rather than silently skipping them. Overlapping
writes are serialized by the manager.
The manager reports blocking findings to the same worker rather than taking
over implementation.

## Review gates

1. `work_manager` publishes one decision-complete worker assignment.
2. The assigned component worker verifies its package with focused tests.
3. `work_manager` reviews the integrated diff and routes blocking remediation
   through the same worker.
4. `doc_writer` confirms the accepted contract and prompt agree without
   implementing Go behavior.
5. `work_manager` owns final validation and confirms token hygiene, dry-run
   immutability, commit ordering, and recovery reporting.

## Work-manager boundary

`work_manager` never writes Go or Flutter code, tests, or shared CLI wiring. It may make
delivery decisions, assign exact file boundaries, inspect diffs, and request
remediation. `backend_worker` is the sole owner of assigned Go code;
`mobile_worker` is the sole owner of assigned Flutter/Dart code and focused
tests. Each active worker contract accepts only `work_manager` assignments,
does not delegate further, and returns focused test evidence plus explicit
unavailable-lane results. `work_manager` reviews the integrated worker diff
and routes every blocking finding back to that same worker; it never takes over
implementation.

`backend_agent` is retained for direct architecture work requested outside the
active work-manager path. It is never a downstream delegate of `work_manager`
and does not assign implementation workers.

## Related

- [[SMT - Implementation Spec]]
- [[../10-development/SMT - Component Developer Toolchains|Component Developer Toolchains]]
- [[../../AGENTS|Repository agent operating agreement]]
