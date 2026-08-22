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
updated: 2026-08-22
---
# SMT — Component Developer Toolchains

## Purpose and status

This is a researched planned contract for generated component repositories; its
component toolchain sections remain planned unless marked implemented. It
refines [[../superpowers/specs/2026-08-17-smt-extensible-modules-design|the module design]] and [[../superpowers/plans/2026-08-17-smt-v0.1.0-production|the v0.1.0 plan]]. The implemented `.5` slice adds the static schema-v1 catalog and validation metadata; `.3.1` adds a deterministic root OCI runtime contract without executing it; `.3.3.2` adds deterministic API source and OpenAPI assets without packaging or runtime execution. Current `smt apply` remains deterministic and offline: it must not install host tools, skills, plugins, MCP servers, dependencies, or runtime configuration, and it rejects non-selectable platform metadata before topology or staging/destination mutation. Future generator work may emit checked-in component declarations and `doctor` guidance.

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
Compose, socket probing, or health checks. Remaining Web/Database build
contexts, Containerfiles, Mobile platform SDK/device verification, lifecycle
tasks, and app-domain behavior remain deferred except for the generated Mobile
source/platform assets and API source/Taskfile contract.

## Planned generated manifest ownership

The Web, Database, and future runtime manifests below are planned starter
behavior, not `.5` or `.3.1` outputs. Mobile `.3.5.1` owns the reviewed
Flutter base-manifest policy: the CLI creates `pubspec.yaml`,
`analysis_options.yaml`, and the project baseline during Apply; `pubspec.lock`
and the pinned lint policy are produced and verified later after `pub get`. The
API `go.mod`/`go.sum`
exception is implemented in `.3.3.1`, and the API source/OpenAPI assets are
implemented in `.3.3.2`; future generator work may copy the remaining reviewed
manifests and lockfiles deterministically and offline.
`smt apply` never runs a package manager.

- **Web** owns `package.json` and `package-lock.json`: runtime Next.js 16.2.9
  plus compatible React; devDependencies include ESLint and `eslint-config-next`,
  Prettier, TypeScript/types, Vitest/Vite React/jsdom, React Testing Library,
  and Playwright. Scripts and configuration make every dependency reachable.
- **API** is the implemented `.3.3.1` manifest exception: when API is selected,
  apply writes deterministic `go.mod` and `go.sum` for module
  `example.com/smt/apis` with `go 1.26.5` and Huma
  `github.com/danielgtaylor/huma/v2 v2.39.1`. pgx
  `github.com/jackc/pgx/v5 v5.10.0` and golang-migrate
  `github.com/golang-migrate/migrate/v4 v4.19.1` are added only for
  API+Database. Tool directives pin govulncheck with `golang.org/x/vuln v1.7.0`
  and golangci-lint with `github.com/golangci/golangci-lint/v2 v2.12.2`; the
  migrate tool is conditional on Database. API-only excludes pgx/migrate, and
  no API emits no API manifests. Direct pinned sums are present, but the full
  transitive closure is deferred. gofmt/vet/test, race, coverage, and fuzz are
  SDK commands, not dependencies; Godex and gopls are not module dependencies.
- **Mobile** is the next P0 starter lane, `smt-4xf.3.5` with children
  `.3.5.1-.3`; Web `.3.2` and `.3.2.1-.3` remain P2/deferred. The lane starts
  independently from current `main`, does not wait for an API PR, and does not
  require Web, API, or Database. Mobile stays backend-independent initially;
  typed API integration follows generated API/Database runtime work. Current
  `smt apply` stages the Flutter CLI `.3.5.2` project; `.3.5.1` lockfile/lint
  production and verification occur later after `pub get`; `.3.5.3`
  verification remains deferred and unverified. The delivery
route is `work_manager -> mobile_worker -> doc_writer`; the Mobile worker owns
only assigned Flutter/Dart production code and focused tests and does not
delegate.
- **Database and root** do not invent language package manifests. Host/runtime
  tools remain prerequisite and Taskfile declarations.

Skills, MCP, browser/live tools, Podman, Task, Gitleaks, and SDK installations
never belong in application manifests.

## Implemented `.3.3.2` API runtime and OpenAPI assets

When API is selected, apply emits deterministic `main.go`,
`internal/server/server.go`, `cmd/openapi/main.go`, `.env.example`, and
`openapi.yaml`, and `Taskfile.yml` in the API child repository alongside its
`.3.3.1` manifests.
No API selection emits no API child or API assets. The module is
`example.com/smt/apis` on Go `1.26.5`, with Huma v2.39.1 through the Gin
adapter, Gin v1.12.0, and Prometheus `github.com/prometheus/client_golang v1.24.1`. API-only source
has no pgx, migrate, or database code; API+Database retains those dependencies
in the manifest only.

The generated runtime uses JSON `slog` and defaults
`HTTP_ADDR=:8080`, `APP_ENV=development`, `LOG_LEVEL=info`,
`HTTP_READ_TIMEOUT=15s`, `HTTP_READ_HEADER_TIMEOUT=5s`,
`HTTP_WRITE_TIMEOUT=15s`, `HTTP_IDLE_TIMEOUT=60s`,
`HTTP_MAX_HEADER_BYTES=1048576`, and `HTTP_SHUTDOWN_TIMEOUT=10s`. Huma
metadata is OpenAPI 3.1, title `SMT API`, version `v0.1.0`, with `/docs`,
`/openapi.json`, `/openapi.yaml`, `/healthz`, `/readyz`, and `/metrics` routes.
`cmd/openapi` constructs the
shared Huma API and writes `api.OpenAPI().YAML()` offline without a listener;
the committed YAML is byte-identical to regeneration across fresh Apply
destinations. Health, readiness, `/metrics`, `X-Request-ID`, panic recovery,
and `SIGINT`/`SIGTERM` graceful shutdown follow the canonical implementation
contract.

The generated server `Config` carries direct `github.com/caarlos0/env/v11
v11.4.1` `env`/`envDefault` tags on its typed fields, including `slog.Level`
for `LogLevel` and `time.Duration` for the timeout fields. `LoadConfig()` calls
plain `env.Parse(&cfg)`; native caarlos/TextUnmarshaler parsing controls
malformed-value errors; no separate semantic conversion or post-parse
validation is added. `Run` logs a structured `configuration load failed`
event and panics with the native parse error before constructing the
application. Normal Gin/Huma runtime and graceful-shutdown behavior is
unchanged. The exact pin and direct checksums are present in both static API
manifest variants.

The API child also receives deterministic `Taskfile.yml` with top-level
`dotenv: ['.env']`; it never copies or mutates `.env`. Its tasks are `build`
(`mkdir -p bin && go build -trimpath -o bin/apis .`), `run` (depends on
`build`, then runs `./bin/apis`), `test`, `coverage`, `mod` (`go mod verify`),
offline byte-comparing `openapi`, and `verify` (depends on `build`, `test`,
`mod`, and `openapi`, then runs `go vet ./...`). API+Database receives the
same API Taskfile but no database migration/readiness tasks yet; those belong
to `smt-4xf.6.1.2` and later. Task v3.52.0 verified dotenv-driven `/healthz`
and bounded process cleanup in the generated-child harness.

Apply writes embedded assets only and performs no network, Go/package-manager
command, tool installation, Task execution, Podman, listener, or runtime
execution. It adds no credentials, domain CRUD, database connectivity/readiness,
migrations, root Taskfile changes, Containerfiles, or non-root packaging. Durable unit/race/fuzz/
integration coverage is `.3.3.3`; non-root packaging/runtime verification is
`.3.3.4`. `go mod tidy` remains a later source-closure check; `go mod verify`
evidence is limited to the generated child `mod` task, and human E2E remains a
later evidence boundary.

## Go API

Baseline: Go 1.26.5, Huma v2.39.1 through Gin v1.12.0, and Prometheus
`github.com/prometheus/client_golang v1.24.1`. The `.3.3.1` manifests and `.3.3.2` source/OpenAPI
assets are static templates; apply performs no Go, `go mod`, package-manager,
network, or tool installation work. Planned gates are gofmt check,
`go vet ./...`, `go test ./...`, race, coverage, focused fuzz,
`govulncheck ./...`, and pinned
golangci-lint v2 configuration/version. `go mod tidy` remains a later check
against the eventual source closure; the generated child `mod` task exercises
`go mod verify` for the emitted manifest.
The `.3.3.2` focused harness checks generated source/build behavior, health,
readiness, metrics, docs/OpenAPI routes, and offline OpenAPI regeneration;
durable unit/race/fuzz/integration suites are `.3.3.3`. Integration tests use
disposable PostgreSQL only when the later database behavior exists, and
migrations remain explicit/API-owned. `golint` is deprecated and frozen; use
go vet and Staticcheck-class checks through golangci-lint instead.
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

## Flutter Mobile manifest and planned runnable lane

Baseline: Flutter `>=3.44.9`, pinned in the generated root as
`flutter 3.44.9-stable`. Implemented `.3.5.1` owns the Flutter base-manifest
policy: the CLI creates `pubspec.yaml`, `analysis_options.yaml`, and the
project baseline with Dart `>=3.12.0 <4.0.0`; `pubspec.lock` and pinned
`flutter_lints 6.0.0` policy are produced and verified later by `mobile_worker`
after `asdf exec flutter pub get`. SDK tools, skills, MCP,
signing, and host prerequisites are not app dependencies.

`.3.5.2` (implemented) — after staging the root `.tool-versions`, Mobile Apply
runs this exact command in the staged child and preserves its CLI output:

```sh
asdf exec flutter --suppress-analytics create --empty --no-pub --platforms=android,ios --org=com.example.smt --project-name=smt_mobile --description="A provider-neutral SMT Flutter mobile starter." <staged-mobile-directory>
```

Flutter owns the generated app/source/test/platform baseline. Apply uses no
static Android/iOS templates and performs no Go post-create app/source/test/
analysis writes. `--no-pub` keeps Apply offline and prevents pub-get or package
resolution. If the pinned toolchain is unavailable, the failure guidance is
`asdf install flutter 3.44.9-stable` followed by `asdf current flutter`, and
the Apply remains atomic. Current evidence is asdf Flutter create, pub get, and
analyze passing. Android SDK absence, incomplete Xcode, and missing CocoaPods
leave device/build lanes unverified. `.3.5.3` (deferred/planned) covers runtime
execution and verification; `.6.1.3` activates only after the Mobile rollup
closes and adds dependency, format, analyze, test, integration, Android debug,
iOS debug, and aggregate `verify` tasks.

The required official source is `flutter/agent-plugins`; a minimal core is
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

## Local Web and Mobile E2E packages

The root-attached `e2e` declaration is being expanded by `smt-4xf.14`. Apply
will generate separate `e2e/web` and `e2e/mobile` packages only when the
matching Web or Mobile component is selected; E2E without either target stays
metadata-only. Web uses Playwright with isolated tests, web-first assertions,
Chromium by default, optional Firefox/WebKit projects, and traces on retry.
Mobile keeps `integration_test` files in the Mobile app for native device
execution while `e2e/mobile` owns the runner, environment, fixtures, and
reports. The first lane asserts stable navigation hooks, Web `/healthz`,
optional API reachability, Mobile launch, and `mobile-home`/`api-status`.

The E2E worker uses `$build-web-apps:frontend-testing-debugging` and
`$flutter-add-integration-test`. It delegates startup/shutdown to existing
component Taskfiles, never installs packages or browsers during Apply, and
reports missing browsers, SDKs, simulators, emulators, or devices explicitly.
Auth, domain fixtures, signing, cloud device farms, remote CI, and production
regression suites remain application-owned follow-up work.

## PostgreSQL / Database

Baseline: PostgreSQL 18. Planned gates use `pg_isready`, fail-fast `psql`,
golang-migrate v4.19.1 up/version checks, API-owned migrations, and disposable
Podman-backed integration tests. No automatic migration, down, drop, or force.
Reuse Godex database guidance; no DB MCP is required for v0.1.0.

## Container / root workspace

The generated API child Taskfile is implemented above. The future root Taskfile
will orchestrate component gates, Database migration/readiness tasks, and
Beads-aware aggregate verification; root task aggregation is `.6.1.2` and
later.
Podman/Compose smoke tests cover build, start, health/readiness, shutdown, and
non-root identity. Gitleaks/security tasks are required before a production
candidate. SBOM, signing, and remote CI are deferred.

## Deferred ownership matrix

| Component | Generated manifest/lockfile | Required local tools | Task gates | Required skills | Optional MCP/runtime |
| --- | --- | --- | --- | --- | --- |
| Go API | `go.mod`, `go.sum`, `.3.3.2` source/OpenAPI assets, `Taskfile.yml` | Go, Huma, Gin, Prometheus, `env/v11`, pgx/migrate when Database, golangci-lint, Task | child `build`, `run`, `test`, `coverage`, `mod`, `openapi`, `verify`; later durable format, vet, race, fuzz, vuln, migrations | `$godex:godex-go-backend` | gopls local; no Go MCP |
| Next.js Web | `package.json`, `package-lock.json` | Node, Next.js, npm lockfile | Prettier, ESLint, TypeScript, Vitest/RTL, build, Playwright | React best practices; frontend testing/debugging | Browser for rendered/E2E |
| Flutter Mobile | `.3.5.1` policy: Flutter CLI `pubspec.yaml`/analysis baseline; lockfile and pinned lint policy after `pub get`; `.3.5.2` implemented: CLI-generated project | Flutter/Dart 3.44.9 stable, Android/iOS debug toolchains | `.3.5.3` deferred/unverified: format, analyze, unit/widget, integration, debug builds; `.6.1.3`: child Taskfile and aggregate verify | Flutter agent-plugin core | Dart MCP/UI driving opt-in |
| Root E2E | `.14` package manifests; Web lockfile after explicit local install | Node/Playwright browser and Flutter/Dart device toolchains | Web contract smoke, Mobile integration smoke, local orchestration, retained reports | `$build-web-apps:frontend-testing-debugging`; `$flutter-add-integration-test` | Browser/device live lanes; no MCP or device farm required |
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
