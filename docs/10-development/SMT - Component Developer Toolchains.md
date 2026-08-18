---
type: technical-contract
status: planned
owner: platform
tags:
  - smt
  - toolchains
  - components
  - taskfile
  - skills
  - mcp
created: 2026-08-17
updated: 2026-08-17
---
# SMT — Component Developer Toolchains

## Purpose and status

This is a researched planned contract for generated component repositories; its
component toolchain sections are not implemented behavior. It refines [[../superpowers/specs/2026-08-17-smt-extensible-modules-design|the module design]] and [[../superpowers/plans/2026-08-17-smt-v0.1.0-production|the v0.1.0 plan]]. The implemented `.5` slice adds the static schema-v1 catalog and validation metadata; `.3.1` adds a deterministic root OCI runtime contract without executing it. Current `smt apply` remains deterministic and offline: it must not install host tools, skills, plugins, MCP servers, dependencies, or runtime configuration, and it rejects non-selectable platform metadata before topology or staging/destination mutation. Future generator work may emit checked-in component declarations and `doctor` guidance.

Each component has four layers: native CLI/toolchain; repeatable Taskfile
gates; agent skills; and optional MCP/live-runtime integration. Taskfiles use
`format:check`, `lint`, `test`, and `verify` conventions. Outputs are
deterministic. Skills teach workflows; MCP provides explicitly opted-in live
context/actions. Required and conditional dependencies are declared at the
component boundary, and MCP configuration must never contain secrets.

## Implemented module declarations

The code-owned schema-v1 catalog contains exactly 11 declarations: selectable
`web`, `mobile`, `api`, `database`, and `e2e`; and non-selectable platform
declarations `container`, `cicd`, `observability`, `iac`, `k8s`, and `argocd`.
Placement modes are declarative and validated as `attached`, `shared`, or
`independent`; the built-in `.5` matrix is:

| Module(s) | Placement | Stable completion criteria |
| --- | --- | --- |
| `web`, `mobile`, `api`, `database` | independent self-targets | `web.declaration`, `mobile.declaration`, `api.declaration`, `database.declaration` |
| `e2e` | attached `repo` | `e2e.declaration` |
| `container` | attached `web` + `api` | `container.declaration` |
| `cicd` | attached `repo` + `web` + `mobile` + `api` + `database` | `cicd.repository-boundary` |
| `observability` | attached `web` + `api` + `database` | `observability.boundary` |
| `iac` | independent `platform/iac` | `iac.provider-neutral` |
| `k8s` | independent `platform/k8s` | `k8s.static-validation` |
| `argocd` | independent `platform/argocd`; requires `k8s` | `argocd.sync-policy` |

Catalog validation covers schema, duplicate/unknown references, safe paths,
capabilities, placement targets, stable completion IDs, and dependency cycles.
Configuration validation covers repository module IDs and required
capabilities. `Config.LoadBytes` accepts known platform metadata when valid,
including `[argocd, k8s]`; `Apply` and `ValidateBlueprint` reject
non-selectable platform metadata before topology or staging/destination
mutation.

The catalog's verification, agent/skill, and scaffold-asset fields are
declarations. The `.5` slice creates no platform repositories, scaffolds, or
platform runtime artifacts; installs no tools, skills, or MCP; mutates no host
configuration; and runs no Compose, Podman, Kubernetes, ArgoCD, or OpenTofu
runtime. Runnable starters and `smt extend` remain deferred.

## Implemented `.3.1` runtime boundary

`smt apply` now emits deterministic root `compose.yaml` and `.env.example`,
and root `.gitignore` ignores `.env`. Compose contains only selected `web`,
`api`, and `database` services; Mobile remains outside OCI Compose. API-only,
Database-only, Web-only, API+Database, all-OCI, empty, and Mobile-only
selections remain valid. Default host bindings are Web `3000:3000`, API
`8080:8080`, and Database `5432:5432`, overridden by `WEB_PORT`, `API_PORT`,
and `DATABASE_PORT`. The project name is the normalized destination basename
in safe lowercase-hyphen form, capped at 63 characters, with
`smt-workspace` fallback.

Web uses `/healthz`; API health/readiness use `/healthz` and `/readyz`; and
Database uses `pg_isready`. Web-to-API and API-to-Database dependencies are
conditional on `service_healthy`. `.env.example` is examples-only with an
empty `DATABASE_PASSWORD=` and no generated credentials.

`runtime.Preflight` is a pure, injectable contract for future Taskfile/CLI use:
it reports invalid or colliding/occupied ports and missing Podman or Podman
Compose prerequisites with actionable guidance. It does not execute external
commands itself. Apply renders the files but does not invoke Preflight, Podman,
Compose, socket probing, or health checks. Component build contexts,
Containerfiles, lifecycle tasks, and app-domain behavior remain deferred to
`.3.2` through `.3.6`.

## Deferred generated manifest ownership

The following are planned scaffold behavior, not `.5` outputs. Future generator
work may copy reviewed manifests and lockfiles deterministically and offline;
`smt apply` never runs a package manager.

- **Web** owns `package.json` and `package-lock.json`: runtime Next.js 16.2.9
  plus compatible React; devDependencies include ESLint and `eslint-config-next`,
  Prettier, TypeScript/types, Vitest/Vite React/jsdom, React Testing Library,
  and Playwright. Scripts and configuration make every dependency reachable.
- **API** owns `go.mod` and `go.sum`: runtime Huma 2.39.1; pgx 5.10.0 and
  migrate 4.19.1 only when API+Database are selected. Go tool directives pin
  govulncheck, golangci-lint v2, and conditional migrate. gofmt/vet/test,
  race, coverage, and fuzz are SDK built-ins, not dependencies. Godex and
  gopls are not module dependencies.
- **Mobile** owns `pubspec.yaml`, `pubspec.lock`, and
  `analysis_options.yaml`: `flutter_test` and `integration_test` SDK
  dependencies plus `flutter_lints` in `dev_dependencies`. dart format,
  flutter analyze/build, and DevTools are SDK tools; Flutter skills and Dart
  MCP are external opt-ins.
- **Database and root** do not invent language package manifests. Host/runtime
  tools remain prerequisite and Taskfile declarations.

Skills, MCP, browser/live tools, Podman, Task, Gitleaks, and SDK installations
never belong in application manifests.

## Go API

Baseline: Go 1.26.5, Huma v2.39.1, pgx v5.10.0. Planned gates are gofmt
check, `go vet ./...`, `go test ./...`, race, coverage, focused fuzz,
`govulncheck ./...`, and pinned golangci-lint v2 configuration/version.
Integration tests use disposable PostgreSQL; OpenAPI generation is checked
for drift and migrations are explicit/API-owned. `golint` is deprecated and
frozen; use go vet and Staticcheck-class checks through golangci-lint instead.
Required skill: `$godex:godex-go-backend`. `gopls` is local navigation, not a
required Go MCP for v0.1.0.

## Next.js Web

Baseline: Next.js 16.2.9 on Node 24.18.0. Use direct
`eslint . --max-warnings=0` because Next 16 removed `next lint`; keep Prettier
check and write separate, with Prettier owning formatting and ESLint owning
code quality. Planned gates are `tsc --noEmit`, Vitest + React Testing
Library, `next build`, Playwright after build/start, and lockfile/dependency
checks. Required skills: `build-web-apps:react-best-practices` and
`build-web-apps:frontend-testing-debugging`. Browser tooling is conditional for
rendered/E2E verification.

## Flutter Mobile

Baseline: Flutter 3.44.9. Planned gates are dart format check, `flutter
analyze`, unit/widget tests, `flutter test integration_test`, Android/iOS
debug builds, and DevTools when runtime diagnosis is needed. The required
official source is `flutter/agent-plugins`; a minimal core is
`flutter-apply-architecture-best-practices`,
`flutter-build-responsive-layout`, `flutter-add-widget-test`, and
`flutter-add-integration-test`, with other skills conditional.

Dart/Flutter MCP is experimental, requires Dart >=3.9, and starts with
`dart mcp-server`. Opt-in Codex setup is:

```sh
codex mcp add dart -- dart mcp-server --force-roots-fallback
```

It may provide analysis/fixes, symbols, formatting, tests, runtime errors,
hot reload, and live-app inspection. Runtime UI driving is additionally opt-in
and must not leak into production configuration.

## PostgreSQL / Database

Baseline: PostgreSQL 18. Planned gates use `pg_isready`, fail-fast `psql`,
golang-migrate v4.19.1 up/version checks, API-owned migrations, and disposable
Podman-backed integration tests. No automatic migration, down, drop, or force.
Reuse Godex database guidance; no DB MCP is required for v0.1.0.

## Container / root workspace

The root runtime contract is implemented, but the future root Taskfile will
orchestrate component gates and Beads-aware verification.
Podman/Compose smoke tests cover build, start, health/readiness, shutdown, and
non-root identity. Gitleaks/security tasks are required before a production
candidate. SBOM, signing, and remote CI are deferred.

## Deferred ownership matrix

| Component | Generated manifest/lockfile | Required local tools | Task gates | Required skills | Optional MCP/runtime |
| --- | --- | --- | --- | --- | --- |
| Go API | `go.mod`, `go.sum` | Go, Huma, pgx, golangci-lint, PostgreSQL | format, vet, test, race, coverage, fuzz, vuln, OpenAPI, migrations | `$godex:godex-go-backend` | gopls local; no Go MCP |
| Next.js Web | `package.json`, `package-lock.json` | Node, Next.js, npm lockfile | Prettier, ESLint, TypeScript, Vitest/RTL, build, Playwright | React best practices; frontend testing/debugging | Browser for rendered/E2E |
| Flutter Mobile | `pubspec.yaml`, `pubspec.lock`, `analysis_options.yaml` | Flutter/Dart, Android/iOS debug toolchains | format, analyze, unit/widget, integration, debug builds | Flutter agent-plugin core | Dart MCP/UI driving opt-in |
| PostgreSQL | None | PostgreSQL, psql, pg_isready, migrate, Podman | readiness, migration up/version, disposable integration | Godex database guidance | No DB MCP in v0.1.0 |
| Root/container | `compose.yaml`, `.env.example` | none for apply; Podman/Compose for deferred runtime | contract inspection; future smoke lifecycle, non-root, security, aggregate verify | project workflow guidance | no runtime execution in `.3.1` |

## Research sources

- [Go security](https://go.dev/doc/security/best-practices); [`golang/lint` deprecation](https://github.com/golang/lint); [golangci-lint](https://golangci-lint.run/)
- [Next.js testing](https://nextjs.org/docs/app/guides/testing); [Next.js ESLint](https://nextjs.org/docs/app/api-reference/config/eslint); [Next.js 16](https://nextjs.org/docs/app/guides/upgrading/version-16)
- [ESLint configuration](https://eslint.org/docs/latest/use/configure/configuration-files); [Prettier](https://prettier.io/docs/install)
- [Flutter testing](https://docs.flutter.dev/testing/overview); [Flutter agent skills](https://docs.flutter.dev/ai/agent-skills); [Flutter MCP](https://docs.flutter.dev/ai/mcp-server)
- [Codex skills](https://learn.chatgpt.com/docs/build-skills); [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli); [React best practices](https://www.skills.sh/vercel-labs/agent-skills/vercel-react-best-practices)

## Related

- [[../README|SMT Documentation]]
- [[../00-project/SMT - Implementation Spec|SMT — Sanovy Mono Tool]]
- [[../00-project/SMT - Agent Team|SMT — Agent Team]]
- [[../superpowers/specs/2026-08-17-smt-extensible-modules-design|SMT Extensible Modules Design]]
- [[../superpowers/plans/2026-08-17-smt-v0.1.0-production|SMT v0.1.0 Production Plan]]
