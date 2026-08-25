---
type: command-recipes
status: active
owner: platform
tags:
  - smt
  - cli
  - development
  - release
created: 2026-07-16
updated: 2026-08-25
---
# SMT — Command Recipes

These examples assume Go plus Task are installed. Build first in the SMT source
checkout when using `bin/smt`; commands that inspect or operate an existing
workspace require a valid `smt.yaml`.

## Create a platform workspace

From the SMT repository root, create the blueprint outside this checkout:

```sh
mkdir -p ../platform-config
bin/smt new ../platform-config/smt.yaml
```

`new` interactively selects independent Web (`nextjs`), Mobile (`flutter`), API
(`go`), and Database (`postgresql`) components. Immediately after Web, it asks
`Include Flutter mobile application? [Y/n]`: Enter includes the Android/iOS
Flutter component; only an explicit no opts out. When Mobile is selected,
repositories are ordered `repo`, `web`, `mobile`, `api`, `database`; an opt-out
omits the Mobile entry. New blueprints have no DevOps prompt,
`workspace.stack.devops`, `infra` repository, or Docker/OpenTofu metadata or
artifacts. After Database, it offers the optional default-no
`Include ZITADEL identity module? [y/N]` question. Identity requires Database,
records only `modules: [identity]` on the root, and adds the local ZITADEL
Compose contract plus generic OIDC environment keys without enforcing login or
generating client secrets. It then offers the optional default-no
`Include E2E quality declaration? [y/N]` question. Opting in records only
`modules: [e2e]` on the root; selecting both records `[identity, e2e]` in prompt
order and component repositories receive exact module IDs.
Current Apply remains metadata-only for E2E; the P0 `smt-4xf.14` rollup will
generate attached `e2e/web` and `e2e/mobile` packages only for selected Web or
Mobile targets, while no-target E2E remains metadata-only. It writes `smt.yaml`
only after confirmation and does not create a workspace. The generated file carries the
exact provenance mapping documented in [[../00-project/SMT - Implementation Spec#Configuration contract|the implementation
specification]], with no timestamp, user, machine/path, Git
SHA, random value, or environment-derived field. Blueprint generation is
byte-stable without network access for identical selections in fresh destinations; the
selected Web Apply initializer is the documented pinned `npx` exception that
may access the npm registry. The destination file must not already exist.
Inspect the generated `smt.yaml` before applying it.

```sh
$EDITOR ../platform-config/smt.yaml
bin/smt apply --config ../platform-config/smt.yaml ../platform
```

`apply` validates the supplied workspace blueprint/configuration, creates a
root repository plus one local submodule per selected component, and writes the
workspace files, local workflow metadata, deterministic `compose.yaml`, and
`.env.example` at a destination that does not already exist. Root `.gitignore`
ignores `.env`. With Mobile selected, output includes the Git-ready
`mobile-app` shell, `mobile_worker` manifest, Flutter README and ignore rules,
the root `.tool-versions` Flutter `3.44.9-stable` pin, and the `.3.5.1`
base-manifest policy. Apply then stages Mobile and runs the exact
Flutter CLI command below for `.3.5.2`, preserving its output:

```sh
asdf exec flutter --suppress-analytics create --empty --no-pub --platforms=android,ios --org=com.example.smt --project-name=smt_mobile --description="A provider-neutral SMT Flutter mobile starter." <staged-mobile-directory>
```

There are no static Android/iOS templates and no Go post-create app/source/
test/analysis writes. Apply invokes no `flutter pub get`, package resolution,
network access, Podman, Compose, signing, or publication. If the pinned asdf
toolchain is unavailable, it reports `asdf install flutter 3.44.9-stable` and
`asdf current flutter` guidance and fails atomically before destination
publication. It does not create remote repositories.
Legacy DevOps-shaped configurations are rejected before destination mutation;
remove the legacy entries and regenerate the blueprint. Generated blueprints
must carry the exact supported provenance; missing, unsupported, or unknown
provenance fails before service or destination mutation. A general
non-generated version-1 configuration without provenance remains usable for
lifecycle and diagnostic commands, but is not applyable as a new generated
blueprint. An existing destination file or directory is refused without
overwrite, merge, regeneration, upgrade, or `smt extend` execution. This
Database `.3.4.1` provides the independent PostgreSQL runtime/readiness child;
`.3.2.1` provides the CLI-owned Web baseline and `.3.2.2/.3` provide Web
quality/runtime tooling.
Mobile `.3.5.3` verification, platform repositories/scaffolds/runtime
artifacts, Podman or Compose execution, a remote module registry, or
`smt extend`; `.3.3.2` adds deterministic API source/OpenAPI assets and `.3.3.4`
adds the API `Containerfile` and child runtime tasks. The generated `compose.yaml` and
`.env.example` are contract-only root artifacts. Generated module annotations
are persisted, but apply does not
execute their verification recipes, install referenced tools/skills/MCP,
mutate host configuration, or create module repositories.

### Accepted Web CLI initializer

Web `.3.2.1` is implemented as a staged, CLI-owned Next.js baseline. The root
`.tool-versions` pins `nodejs 24.18.0`. When Web is selected, Apply invokes
this exact argument-array command before publishing the child:

```sh
asdf exec npx --yes create-next-app@16.2.9 <staged-web-directory> --typescript --eslint --app --empty --tailwind --use-npm --skip-install --disable-git --agents-md --import-alias=@/*
```

Apply preserves the CLI files, merges its `.gitignore`, publishes no
`package-lock.json`, and performs no `npm install` or dependency resolution.
The staged operation is atomic; on failure the destination is not published
and the error gives `asdf install nodejs 24.18.0`, `asdf current nodejs`, and
`asdf exec npx --yes create-next-app@16.2.9 --help` guidance. A selected Web
Apply also emits Web-specific `web_worker` routing and the required
`build-web-apps:react-best-practices` and
`build-web-apps:frontend-testing-debugging` skill references.

The pinned `npx create-next-app` call is the sole Apply exception that may
access the npm registry. Non-Web and static Apply paths remain offline; Web
still performs no installation, lockfile publication, or dependency
resolution.

After Apply, work from `web-app/` with:

```sh
asdf exec npm install
asdf exec npm run dev
```

The later Web worker lanes `.3.2.2/.3` own npm lockfile creation, quality,
browser, and runtime verification. Do not treat this initializer as evidence
that those real npm, browser, or runtime lanes have run.

### Plan the Mobile-first starter lane

The next P0 lane is Mobile rollup `smt-4xf.3.5` with children `.3.5.1-.3`.
Web rollup `smt-4xf.3.2` may remain P2, with `.3.2.1` implemented as the CLI
baseline and `.3.2.2/.3` remaining P2/deferred; existing dependency edges are
unchanged. Mobile starts independently from current
`main`, does not wait for an API PR, and does not require Web, API, or Database.
Keep it backend-independent initially; typed API integration follows generated
API/Database runtime work. The delivery route is
`work_manager -> mobile_worker -> doc_writer`; `mobile_worker` owns only
assigned Flutter/Dart production code and focused tests and does not delegate.

The planned milestones are:

- `.3.5.1` (implemented): Flutter owns the Mobile base `pubspec.yaml`,
  `analysis_options.yaml`, and project baseline for Flutter `>=3.44.9` with
  Dart `>=3.12.0 <4.0.0`. The `pubspec.lock` and pinned `flutter_lints 6.0.0`
  policy are produced and verified later by `mobile_worker` after
  `asdf exec flutter pub get`; Apply's `--no-pub` emits no lockfile and never
  performs package resolution. The README gives the pinned
  `asdf install flutter 3.44.9-stable` and `asdf current flutter` recovery path.
- `.3.5.2` (implemented): after staging root `.tool-versions`, Mobile Apply
  runs the exact `asdf exec flutter --suppress-analytics create --empty
  --no-pub --platforms=android,ios --org=com.example.smt
  --project-name=smt_mobile --description="A provider-neutral SMT Flutter
  mobile starter." <staged-mobile-directory>` command. Flutter owns the
  generated platform output; the Mobile verification worker adds the stable
  app, optional API config, unit/widget tests, native integration test, and SDK
  dependency declaration. Apply uses no static Android/iOS templates. The app
  remains backend-independent with optional non-secret
  `SMT_API_BASE_URL` configuration and provider-neutral `com.example.smt.mobile`;
  signing, store metadata, domain CRUD, OCI services, and API-required behavior
  remain excluded.
- `.3.5.3` (implemented): the generated verification contract includes `dart
  format`, `flutter analyze`, unit/widget tests, native `integration_test`,
  Android debug build, and iOS debug build with `--no-codesign` where supported.
  The opted-in lane passes format, analyze, and unit/widget tests; this host
  has no Android SDK or supported Android/iOS target, so integration and debug
  builds are explicit unverified results, never silently skipped.
- `.6.1.3`: after the Mobile rollup closes, activate the Mobile Taskfile with
  dependency, format, analyze, test, integration, Android debug, iOS debug,
  and aggregate `verify` tasks.

Apply runs only the staged Flutter create plus static Mobile verification-file
writes; it does not run `pub get` or these verification commands. Current
evidence is asdf Flutter create, `pub get`, Dart format, `flutter analyze`, and
unit/widget tests passing. Android SDK absence and the lack of a supported
Android/iOS target leave integration and debug-build lanes unverified.

`.3.5.3` local verification checks:

```sh
cd ../platform/mobile-app
dart format --set-exit-if-changed .
flutter analyze
flutter test
flutter test integration_test
flutter build apk --debug
flutter build ios --debug --no-codesign
```

An optional non-secret API base can be supplied through the app's supported
compile/runtime configuration, for example `SMT_API_BASE_URL`; the app must
still run without an API. Record unavailable SDK or device lanes explicitly.

### Inspect the generated runtime contract

After applying a blueprint, inspect the offline contract without starting any
runtime:

```sh
sed -n '1,220p' ../platform/compose.yaml
cat ../platform/.env.example
grep -n '\.env' ../platform/.gitignore
```

Compose service IDs are `web`, `api`, `database`, and optional `zitadel`
in that order when selected; the identity contract also emits idempotent
`zitadel-db-init`, `zitadel-login`, and `proxy` services. Mobile is not an OCI
service. API-only, Database-only, Web-only, API+Database, all-OCI, empty, and
Mobile-only selections remain valid, with only selected OCI services emitted.
Default host bindings are `3000:3000`, `8080:8080`, `5432:5432`, and
`8081:80` for the local ZITADEL proxy; override them with `WEB_PORT`,
`API_PORT`, `DATABASE_PORT`, and `ZITADEL_PORT`. When an override is unset,
the generated `.env.example` derives deterministic workspace-scoped host
ports so workspaces can run in parallel; explicit overrides remain
authoritative. The project name is the safe lowercase-hyphen destination
basename, capped at 63 characters, with `smt-workspace` fallback. Generated
local resource names are scoped as `<project>-postgres-data`,
`<project>-zitadel`, and `<project>-zitadel-bootstrap`.

Web probes `/healthz`; API probes `/healthz` and `/readyz`; Database probes with
`pg_isready`. Web-to-API and API-to-Database dependencies are conditional and
use `service_healthy`. `.env.example` contains examples only, including the
generated local resource names and the local-development
`DATABASE_PASSWORD=smt-dev-password` value. Replace it outside disposable
local use; no `.env` file is generated.

When Web, API, or Database is selected, Apply also emits the root Compose
Taskfile entrypoints `compose:config`, `compose:build`, `compose:up`,
`compose:down`, and `compose:ps`. These commands pass the root environment
explicitly with `podman compose --env-file .env -f compose.yaml ...`; they do
not rely on implicit Compose discovery. Initialize the operator-owned file
before starting a Database workspace:

```sh
cp .env.example .env
# review or replace DATABASE_PASSWORD, then run
task compose:config
task compose:build
task compose:up
```

For an identity-enabled workspace, the generated Traefik file-provider
configuration routes the configured `ZITADEL_DOMAIN` to the ZITADEL API and
login services. No machine-specific container-engine socket configuration is
required before `task compose:up`.

If the root `.env` is missing, the generated task fails before invoking Podman
with copy/set guidance. Apply continues to generate only `.env.example`; no
credential or `.env` file is created.

The pure preflight API has actionable invalid-port, selected-port collision,
occupied-port, missing-Podman, and missing-Podman-Compose errors for future
Taskfile/CLI consumers. `smt apply` does not invoke that preflight, Podman,
Compose, socket probing, or health checks.

### Inspect the generated Database runtime

When Database is selected, inspect its static runtime contract without starting
Podman:

```sh
sed -n '1,220p' ../platform/database/Containerfile
cat ../platform/database/.env.example
sed -n '1,260p' ../platform/database/Taskfile.yml
```

The child uses PostgreSQL `18-alpine`, a named volume, `pg_isready` health and
readiness checks, and fail-fast `psql` diagnostics. Copy `.env.example` to a
local `.env`, review or replace the `POSTGRES_PASSWORD=smt-dev-password`
example before `task run`; no `.env` is generated or committed. The child contains no application schema or migration
commands. Apply writes these files only; Podman, PostgreSQL, and Task remain
operator-run checks owned by the later Database lifecycle task.

### Inspect generated API manifests

.3.3.1 implements deterministic static API child `go.mod` and `go.sum`
manifests when API is selected; inspect them without downloading or resolving
modules:

```sh
sed -n '1,220p' ../platform/apis/go.mod
cat ../platform/apis/go.sum
```

The module is `example.com/smt/apis` with `go 1.26.5`. API-only contains Huma
`github.com/danielgtaylor/huma/v2 v2.39.1` plus tool directives for govulncheck
(`golang.org/x/vuln v1.7.0`) and golangci-lint
(`github.com/golangci/golangci-lint/v2 v2.12.2`). API+Database additionally
contains pgx `github.com/jackc/pgx/v5 v5.10.0`, golang-migrate
`github.com/golang-migrate/migrate/v4 v4.19.1`, and the migrate tool directive.
API-only excludes pgx/migrate; no API selection emits no API manifests.

Direct pinned sums are present in `go.sum`, but this slice does not prove the
full transitive closure. Apply writes static templates only: it does not invoke
`go`, `go mod`, a package manager, the network, or tool installation. `gofmt`,
vet, test, race, coverage, fuzzing, Godex, and gopls remain SDK/editor/agent
tools rather than module dependencies. `go mod tidy` remains a later check
against the eventual source closure; the generated child `mod` task exercises
`go mod verify` for the emitted manifest.

### Inspect generated API runtime and OpenAPI assets

`.3.3.2` emits these deterministic files only when API is selected:
`main.go`, `internal/server/server.go`, `cmd/openapi/main.go`, `.env.example`,
and `openapi.yaml`, alongside `go.mod` and `go.sum`. No API selection emits no
API child or API assets. The generated module is `example.com/smt/apis` on Go
`1.26.5`; its API-only source uses Huma v2.39.1 through Gin v1.12.0 and
Prometheus `github.com/prometheus/client_golang v1.24.1`. API+Database keeps pgx/migrate in the
manifest only and does not add database code to the generated source.

Runtime defaults are `HTTP_ADDR=:8080`, `APP_ENV=development`,
`LOG_LEVEL=info`, `HTTP_READ_TIMEOUT=15s`, `HTTP_READ_HEADER_TIMEOUT=5s`,
`HTTP_WRITE_TIMEOUT=15s`, `HTTP_IDLE_TIMEOUT=60s`,
`HTTP_MAX_HEADER_BYTES=1048576`, and `HTTP_SHUTDOWN_TIMEOUT=10s`. The
generated server `Config` carries direct `github.com/caarlos0/env/v11
v11.4.1` `env`/`envDefault` tags on its typed fields, including `slog.Level`
for `LogLevel` and `time.Duration` for the timeout fields. `LoadConfig()` calls
plain `env.Parse(&cfg)`; native caarlos/TextUnmarshaler parsing controls
malformed-value errors; no separate semantic conversion or post-parse
validation is added. `Run` logs a structured `configuration load failed`
event and panics with the native parse error before constructing the
application. Normal Gin/Huma runtime and graceful-shutdown behavior is
unchanged; the exact pin and checksums are in `go.mod`/`go.sum`. Huma
publishes OpenAPI 3.1 title `SMT API`, version `v0.1.0`, at `/docs`,
`/openapi.json`, and `/openapi.yaml`. The offline command constructs the shared
Huma API and writes `api.OpenAPI().YAML()` without a listener:

```sh
sed -n '1,220p' ../platform/apis/internal/server/server.go
sed -n '1,120p' ../platform/apis/cmd/openapi/main.go
cat ../platform/apis/.env.example
cat ../platform/apis/openapi.yaml
(cd ../platform/apis && GOPROXY=off GOSUMDB=off go run ./cmd/openapi > /tmp/smt-openapi.yaml && cmp -s /tmp/smt-openapi.yaml openapi.yaml)
```

The committed `openapi.yaml` is byte-identical to regeneration across fresh
Apply destinations. `/healthz` returns 200 `ok`; API-only `/readyz` returns 503
`not_ready` before bootstrap or during shutdown and 200 `ready` after
bootstrap; `/metrics` shares the listener and exposes Go/process plus bounded
request metrics. Safe `X-Request-ID` values are accepted or generated and
returned. Panic recovery logs panic/stack/route/method/request ID via JSON
`slog` and returns generic 500; SIGINT/SIGTERM performs timed graceful
shutdown.

For the approved API+Database `.3.4.3` lane, readiness is narrower and more
strict. Startup requires a valid `DATABASE_URL`; an empty or malformed value is
an actionable configuration error. `/healthz` remains 200 `ok`, while
`/readyz` starts at 503 `not_ready`, a continuous database health loop pings
PostgreSQL every 1 second with a 2 second timeout, readiness changes to 200
`ready` once connectivity is confirmed, and it returns to 503 if connectivity
is later lost. This readiness check proves connectivity only. Startup does not
run migrations.

API-selected Apply only writes embedded assets. It performs no network,
Go/package-manager command, tool installation, Task execution, Podman, listener,
or runtime execution. It adds no credentials, domain CRUD, data-service
connectivity/readiness, or root Taskfile changes.

### Use the generated API child Taskfile

API selection emits `Taskfile.yml` in `../platform/apis`; no API selection emits
no API Taskfile. The file has top-level `dotenv: ['.env']` and does not copy or
mutate `.env`. Its task surface is:

| Task | Behavior |
| --- | --- |
| `build` | `mkdir -p bin && go build -trimpath -o bin/apis .` |
| `run` | Depends on `build`, then runs `./bin/apis`; `.env` supplies the variables parsed by `LoadConfig()`. |
| `test` | `go test ./...` |
| `coverage` | `go test ./... -coverprofile=coverage.out` |
| `test:race` | `go test -race ./...` |
| `test:fuzz` | Runs the bounded request-ID fuzz target. |
| `format:check` | Lists Go files needing formatting and fails when any are found. |
| `lint` | `go tool golangci-lint run ./...` using the pinned tool directive. |
| `vuln` | `go tool govulncheck ./...` using the pinned tool directive. |
| `vet` | `go vet ./...` |
| `mod` | `go mod verify` |
| `openapi` | Offline `GOPROXY=off GOSUMDB=off go run ./cmd/openapi`, compared byte-for-byte with `openapi.yaml`. |
| `container:build` | Development image: caller-installed Podman builds the generated `Containerfile` with `--pull=missing`, fetching absent pinned base images. |
| `container:build:production` | Production image: builds with `--pull=never` and `${SMT_API_PRODUCTION_IMAGE:-smt-api:production}`, requiring preloaded and verified base images. |
| `container:verify` | Podman verifies non-root identity, `/healthz`, `/readyz`, and graceful stop with a bounded wait. |
| `migrate:create NAME=...` | API+Database only: runs the pinned PostgreSQL-tagged `go run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir migrations -seq "$MIGRATION_NAME"`; it requires an explicit migration name. |
| `migrate:up` | API+Database only: applies pending migrations with the pinned PostgreSQL-tagged migrate command and explicit `DATABASE_URL`. |
| `migrate:version` | API+Database only: reports the current version with the pinned PostgreSQL-tagged migrate command and explicit `DATABASE_URL`. |
| `migrate:validate` | API+Database only: runs `up` and then `version` with the PostgreSQL-tagged tool, preserving native failures without rollback. |
| `verify` | API-only depends on static quality, Go tests, OpenAPI, and container tasks; API+Database omits DB-dependent `container:verify`, leaving live lifecycle proof to the `.3.4.3` runbook. |

API+Database receives the migration tasks above, the blank operator-provided
`DATABASE_URL=` example, and a deterministic no-op baseline pair. API-only and
Database-only outputs contain none of these migration assets or commands. The
operator remains responsible for `migrate:up`, `migrate:version`, and
`migrate:validate`; `.3.4.3` does not move them into startup. Task v3.52.0
verified the generated-child harness, including dotenv-driven `/healthz` and
bounded process cleanup. `smt apply` writes the child Taskfile and
`Containerfile` but never runs Task, builds an image, or starts a runtime; there
are no implicit installs or network work, and the root Taskfile is unchanged.
Durable unit/race/fuzz/integration coverage remains `.3.3.3`; non-root
packaging/runtime verification is implemented in `.3.3.4`; and live image
execution remains an explicit environment-dependent lane, not an Apply-time
claim.

The `.3.3.4` API child `Containerfile` uses the pinned
`golang:1.26.5-alpine` builder and `alpine:3.22` runtime, produces a static
trimmed binary, runs as UID/GID `10001`, exposes `8080`, and uses `SIGTERM`.
The image health check covers `/healthz` and `/readyz`. If Go, the pinned Go
tools, Task, or Podman is unavailable, record the relevant check as unavailable
and follow the manual setup guidance in the generated API README. Apply does
not install prerequisites, build an image, start a container, or run a task.

The command above is a local regeneration recipe. It is not evidence of
`go mod tidy` or human E2E completion; `go mod verify` evidence is limited to
the generated child Task harness.

### Manual `.3.4.3` lifecycle runbook

Use this runbook only on a host that can run disposable Podman workloads. As
of Tuesday, August 25, 2026, these docs do not claim that the `.3.4.3` live
runtime lane has already passed in this repository.

Reserve unique disposable resources for each scenario and never reuse or clean
up unrelated existing containers, volumes, or networks. Clean successful
scenarios. Preserve failed resources and logs for diagnosis, and keep captured
evidence secret-safe by redacting passwords, DSNs, and machine-specific paths.

Prepare one Database-only workspace and one API+Database workspace from fresh
blueprints, then copy each generated `.env.example` to `.env` and replace only
the disposable local values required for that scenario. The Database-only lane
verifies container build/start, `pg_isready`, fail-fast `psql`, stop, rerun
with no config drift, and cleanup. The API+Database lane verifies:

- startup fails closed when `DATABASE_URL` is empty or malformed
- `/healthz` stays 200 while `/readyz` remains 503 until PostgreSQL
  connectivity is confirmed
- `/readyz` becomes 200 after connectivity, returns to 503 on connectivity
  loss, and recovers to 200 after restart/recovery
- readiness reflects connectivity only and does not imply migration success
- operator-owned `migrate:up`, `migrate:version`, and `migrate:validate`
  behavior for the baseline no-change rerun, a failing migration, a dirty
  migration state, and concurrent migration lock contention

Record the exact disposable resource names used per scenario, the commands
run, the observed readiness transitions, whether cleanup completed, and the
location of any preserved logs. If Podman is installed but the local runtime is
unavailable, record that environment limitation explicitly instead of marking
the lane verified. The broader root Compose matrix remains tracked by
`smt-4xf.3.6`, so this runbook stays at the child-service level.

### Inspect module declarations

The implementation specification documents the static schema-v1 catalog and
the exact generated annotations. The implemented catalog is code-owned, not
user YAML, and contains exactly 11 declarations: selectable `web`, `mobile`,
`api`, `database`, and `e2e`; and non-selectable platform declarations
`container`, `cicd`, `observability`, `iac`, `k8s`, and `argocd`.

Placement modes are declarative and validated as `attached`, `shared`, or
`independent`. The `.5` catalog uses this authoritative matrix:

| Module(s) | Placement mode and targets | Stable completion criterion IDs |
| --- | --- | --- |
| `web`, `mobile`, `api`, `database` | independent self-targets | `web.declaration`, `mobile.declaration`, `api.declaration`, `database.declaration` |
| `e2e` | attached to `repo` | `e2e.declaration` |
| `container` | attached to `web` + `api` | `container.declaration` |
| `cicd` | attached to `repo` + `web` + `mobile` + `api` + `database` | `cicd.repository-boundary` |
| `observability` | attached to `web` + `api` + `database` | `observability.boundary` |
| `iac` | independent at `platform/iac` | `iac.provider-neutral` |
| `k8s` | independent at `platform/k8s` | `k8s.static-validation` |
| `argocd` | independent at `platform/argocd`; requires `k8s` | `argocd.sync-policy` |

The full matrix also records each declaration's path and scope in [[../00-project/SMT - Implementation Spec|the implementation specification]]. Catalog validation covers schema, duplicate/unknown module and capability references, safe paths, placement targets, stable completion IDs, and capability dependency cycles. Configuration validation rejects unknown or duplicate repository module IDs and missing required capabilities.

`Config.LoadBytes` accepts known non-selectable platform metadata when the
references and dependencies are valid, so `[argocd, k8s]` is loadable while
`[argocd]` is rejected for its missing capability. `Apply` and
`ValidateBlueprint` reject non-selectable platform metadata before topology
checks or staging/destination mutation. The root-only `modules: [e2e]`
declaration is currently metadata; package generation is tracked by
`smt-4xf.14`. Its Web lane uses Playwright for contract smoke and `/healthz`;
its Mobile lane uses Flutter's native `integration_test` with
`mobile-home`/`api-status` keys. Local tasks delegate startup to selected
component tasks and report missing browsers, SDKs, or devices explicitly.

This `.5` slice adds declarations and validation only. Apply does not execute
verification commands, install tools/skills/MCP, mutate host configuration,
create platform repositories/scaffolds/runtime artifacts, or run
Compose/Podman/Kubernetes/ArgoCD/OpenTofu. Runnable starters and `smt extend`
remain deferred.

Each created repository receives a scaffold-only `lefthook.yml` with top-level
`no_auto_install: true` and `assert_lefthook_installed: true`. Its `commit-msg`
entry calls bare `smt validate-message --config FILE {1}`, where `FILE` is the
correct relative path to the root `smt.yaml`. `no_auto_install` prevents
Lefthook from automatically installing or updating hooks when configuration
changes; the assertion makes Git fail if Lefthook cannot be found, rather than
silently skipping validation. Applying a blueprint does not execute Lefthook or
install a Git hook.

### Beads bootstrap files

The initial workspace commit includes Beads configuration and metadata while
honoring the `.beads/.gitignore` created by `bd init`. The embedded Dolt
database, locks, backups, and other local runtime files remain on disk but are
not tracked by Git. Verify a generated workspace with:

```sh
git ls-files -ci --exclude-standard
git check-ignore -v .beads/embeddeddolt/
```

For a workspace created by an older SMT version, preserve the local Beads
database and remove only indexed ignored paths from Git:

```sh
bd doctor --fix
git ls-files -ci --exclude-standard
git rm --cached -r .beads/embeddeddolt/
git rm --cached .beads/.local_version
git commit -m "fix(repo): stop tracking Beads runtime data"
```

Use the output of `git ls-files -ci --exclude-standard` to include any other
indexed ignored runtime paths; do not delete the local files or rewrite Git
history.

## Human E2E Mobile review handoff

The pending human review (`smt-3r2.5`) should create one default Mobile
blueprint (press Enter) and one explicit opt-out blueprint, then apply each in
new destinations. Verify the default YAML order and Mobile artifacts listed
above, including the exact provenance mapping from the implementation spec;
verify the opt-out contains no Mobile stack or repository. Repeat identical
selections in fresh destinations and compare bytes to confirm deterministic
offline generation. For the selected Mobile destination, confirm the preserved
staged Flutter CLI command, then run `asdf install flutter 3.44.9-stable`,
`asdf current flutter`, `asdf exec flutter pub get`, and `asdf exec flutter
analyze`; then run the Android or iOS device lane when its SDK and device are
available. Do not claim signing or store publication. At one additional fresh
destination, exercise
one safe prerequisite, staging, Beads, or publish failure and verify that no
partial destination remains.

Add credential-free remote URLs after applying the blueprint:

```yaml
repositories:
  - id: web
    path: web-app
    remote:
      url: git@github.com:example/web-app.git
```

The generated `.gitmodules` initially records local bootstrap URLs. For
provider-backed delivery, declare exact projects and let SMT discover/create
and wire them:

```yaml
providers:
  github:
    api_base_url: https://api.github.com/
repositories:
  - id: repo
    path: .
    provider: github
    project: acme/platform
    scope: repo
    visibility: private
```

```sh
export SMT_GITHUB_TOKEN=...
bin/smt remote provision --dry-run
bin/smt remote provision --json
```

Provisioning uses child-first provider discovery/creation and private
visibility by default. It refuses incompatible existing projects or occupied
local origins, never deletes remote projects, and only updates `smt.yaml`,
`.gitmodules`, and Git origins after every target is available. Tokens are read
from `SMT_GITHUB_TOKEN` or `SMT_GITLAB_TOKEN` and are never written to disk.

## Discover commands and create Beads tickets

```sh
bin/smt --help
bd prime
bd create --title="Short task title" --description="Why this exists and what needs to be done" --type=task --priority=2
bd show <id>
bd update <id> --claim
bd ready
bd blocked
bd close <id> --reason="Completed"
```

Agents create and manage feature or task tickets directly with Beads; SMT does
not wrap ticket creation, review queues, release readiness, or ready-work
listing. Create the implementation ticket before editing code. Use `smt
prepare` only for repository lifecycle coordination; it may create its special
internal `Prepared workspace` task.

## Push configured repositories

```sh
bin/smt push --dry-run
bin/smt push
```

The dry run validates every root/submodule worktree and prints each current
branch in execution order without contacting a remote. A real push rejects
missing remote URLs, dirty or detached repositories, and uninitialized paths;
it pushes child repositories first and the root last. SMT never stages,
commits, force-pushes, or rolls back a successful child push.

## Beads branch lifecycle

```sh
bin/smt prepare
bin/smt switch
bin/smt switch smt-123
bin/smt pull
```

`prepare` has no positional arguments, creates and reports the open `Prepared
workspace` task before running complete preflight, and leaves that task open
when preflight fails without mutating Git. It stashes tracked and untracked
changes but leaves ignored files in place. `switch` with no argument returns
every repository to its effective default branch; `switch BEAD_ID` uses only an
existing local branch. Neither form creates, auto-pops, or rolls back. `pull`
fast-forwards child repositories before the root. The effective default branch
is per-repository `remote.default_branch`, then `main`.
Default branches use ordinary conventional-commit syntax; non-default active
Beads branches require the exact branch ID as `type(scope): [BEAD-ID] summary`.
The root has no special manifest exception. Hooks require Beads readiness.

The former workspace `prepare/submit` manifest flow, Jira aliases, assignment
waves, and provider review automation are removed from the active CLI.

## Create a synchronized linked worktree

```sh
bin/smt worktree add ../platform-feature --branch feature/demo --dry-run
bin/smt worktree add ../platform-feature --branch feature/demo
```

The branch must be new in every configured repository. SMT verifies clean,
attached, initialized root/submodule state plus matching root gitlinks before
creating the root worktree and then nested child worktrees. If an unexpected
child creation fails, SMT reports the created and pending paths for manual
recovery; it does not delete worktrees automatically.

<!-- INACTIVE HISTORICAL WORKSPACE PREPARE/SUBMIT AND PROVIDER REVIEW RECIPES
## Prepare an assigned feature workspace

Resolve one active Beads feature and prepare its synchronized workspace before
starting implementation:

```sh
smt workspace prepare smt-feature ../platform-feature --branch feature/demo --dry-run
smt workspace prepare smt-feature ../platform-feature --branch feature/demo
```

Preparation selects direct active dependency-ready children with exactly one
matching `repo:<id>` label, groups them in configuration order, and records
their titles, descriptions, designs, acceptance criteria, Beads IDs, and
optional Jira-shaped aliases. It creates the root worktree before children and
writes `.smt/runs/smt-feature.json` only after all worktrees succeed. The run
manifest is ignored, contains no credentials, and is the authority for
repository ownership, check-profile names, integration gates, and accepted
commit references. The root records `ownership: integration-worker` and
`integration_gate: root`; children record `ownership: repository-worker` and
`integration_gate: root-gitlink`.

Inside that prepared workspace, commit subjects must use:

```text
feat(api): [smt-123] add endpoint
fix(web): [WEB-456] handle empty response
```

The bracketed ID is mandatory immediately after the conventional prefix.
Child commits accept only their assigned Beads ID or Jira alias. Root
integration/gitlink commits may additionally use the feature ID and assigned
root-task IDs. Missing, malformed, wrong-repository, ambiguous, or corrupt
manifest state fails closed with safe remediation. Outside a prepared
workspace, normal configured conventional-commit validation remains unchanged.

## Submit a prepared workspace

```sh
bin/smt workspace submit smt-feature --dry-run --json
bin/smt workspace submit smt-feature
bin/smt workspace submit smt-feature --ready
```

Submission selects only assigned commits ahead of each manifest base and
requires clean attached worktrees, matching configured origins, reachable
target branches, valid assigned references, and passing `submit` checks before
the first push. Child repositories are pushed before the root; a changed child
must have its gitlink integrated by a root commit. Pushes never force-update or
roll back remote state.

With a configured provider token, SMT reuses an open review with the same
source/target branches or creates a draft PR/MR. `--ready` creates or promotes
a ready review. If a token is absent, the push still succeeds and the command
prints a provider creation link with copy-ready title/body content and exact
`Closes \`WORK-ID\`` lines. A root review waits until selected child review
URLs are available. Repeating the command reuses matching open reviews.

## Traceable workspace release-gate handoff

The following checks remain human-owned release evidence. Run them in
disposable workspaces with representative root and child repositories. Do not
put tokens in commands, terminal captures, Beads notes, or committed files.

### `smt-5w0.11.7` — prepared workspace traceability

1. Create or select a real feature with one repository-scoped Beads child in
   the root and one child repository, plus one assigned Beads task with a
   Jira-shaped `external_ref`.
2. Run `smt workspace prepare FEATURE PATH --branch BRANCH` and retain the
   exact output. Inspect the ignored `.smt/runs/FEATURE.json` manifest and
   confirm the base branch and commit, repository configuration order,
   ownership, assigned Beads IDs, and Jira alias are present and secret-free.
3. In the child repository, attempt an empty commit with a missing ID and a
   commit using the other repository's assigned ID. Both must fail before a
   commit is created. Repeat with the assigned Beads ID and assigned Jira
   alias; both must succeed.
4. In the root repository, create the valid integration/gitlink commit using
   the parent feature ID. Confirm that an arbitrary valid-looking ID is
   rejected and that a corrupt, missing, or ambiguous manifest fails closed.
5. Repeat one preparation as `--dry-run` and confirm it creates no worktree or
   manifest. Record the exact commands, exit statuses, manifest inspection,
   and hook output in `smt-5w0.11.7`; agents must not close that ticket.

### `smt-5w0.13.6` — mixed-provider delivery

1. Use disposable private GitHub and GitLab projects or approved sandbox
   namespaces. Configure fully qualified projects, leave visibility private,
   and export provider tokens only in the current shell.
2. Run `smt remote provision --dry-run`, then the real command. Confirm
   child-first discovery/creation or exact compatible reuse, SSH origins,
   `.gitmodules`, persisted `remote.url` values, and safe created/existing/
   configured/pending reporting. Repeat it to verify idempotency.
3. Prepare a feature and repeat the invalid, cross-repository, assigned
   Beads, assigned Jira-alias, and root integration commit checks from the
   `.11.7` handoff. Create changes in an assigned child and the required root
   gitlink commit only.
4. Unset one provider token and run `smt workspace submit FEATURE`. Confirm
   child-first pushes succeed, the missing provider is reported as a warning,
   copy-ready review text and a safe provider link are printed, and the root
   review is deferred until child review URLs exist.
5. Restore the token and run submission again, once normally and once with
   `--ready`. Confirm draft review creation or reuse, exact `Closes` lines,
   child links in the root review, and no duplicate reviews on rerun. Record
   exact commands and review URLs without credentials; agents must not close
   `smt-5w0.13.6`.

## Inspect and install workspace hooks -->

## Inspect and install workspace hooks

```sh
# From the SMT source checkout.
task build
export PATH="$PWD/bin:$PATH"
# Return to the target/generated workspace.
cd ../platform
smt doctor
smt hooks install --dry-run
smt hooks install
smt status
smt status --json
```

The normal `status` report is for people: it has an overall label, a repository
table, configured profiles and contract counts, and safe next steps. Its JSON
form is for automation and returns the repository entries, profiles, and
contract counts. When no profiles are configured, the human report says
`profiles: none`; JSON retains `profiles: []`.

`doctor` is read-only and renders a repository-first readiness tree. Each
configured repository is expanded in configuration order with `worktree`,
`hook`, `remote`, and `provider` nodes; `tools` and `credentials` are separate
roots. It reports token presence only, never token values, and places the
remediation directly beneath the affected warning or error. Missing remotes
and provider tokens are warnings; absent providers are valid local-only state.

For example, a safe redirected report is shaped like this:

```text
DOCTOR ! WARN
workspace
├─ repo ✓ ready
│  ├─ worktree ✓ initialized
│  ├─ hook ✓ current
│  ├─ remote ✓ configured
│  └─ provider ✓ github · acme/repo
└─ api ! warning
   ├─ worktree ✓ initialized
   ├─ hook ! absent
   │  └─ fix: run smt hooks install
   ├─ remote ! not configured
   │  └─ fix: configure remote.url before remote operations
   └─ provider ✓ local-only
tools
└─ git ✓ available
credentials
└─ github ! token missing
   └─ fix: set SMT_GITHUB_TOKEN before provider operations
```

`READY` means all checks passed, `WARN` means work can continue with a
non-blocking issue, and `ERROR` means a required local check failed. A
worktree is the Git checkout SMT inspects; a hook is the commit-msg validation
installed there; a remote is the configured Git destination; a provider is the
optional GitHub/GitLab project; and a credential is the environment token used
for provider APIs. `NO_COLOR` and redirected output remain deterministic.

`task build` creates `bin/smt` in the SMT source checkout; it does not put bare
`smt` on `PATH`. Keep that inherited PATH while returning to the target
workspace. Both bare `smt` and Lefthook must remain available when Git runs a
`commit-msg` hook. Use an equivalent durable PATH setup for an IDE, GUI client,
or other launch environment; retain the bare-command design rather than
replacing it with an absolute path. `smt hooks --help` repeats that both bare
`smt` and Lefthook must be on `PATH`.

For each configured repository, `commit-msg` is `absent` when no hook exists,
`current` when it exactly matches a recognized historical SMT script or the
reviewed Lefthook 2.1.10 dispatcher, and `unmanaged` when it is custom,
lookalike, modified, symlinked, a directory, or another nonregular target. An
absent hook is a warning, not a failed `doctor` run, when the required readiness
checks pass. Unmanaged targets must be resolved manually first: SMT does not
follow or replace them and has no force or chaining mode. An exact recognized
legacy SMT hook is eligible for migration only when no `commit-msg.old` entry
exists. Lefthook 2.1.10 may then preserve it as `commit-msg.old` while it
installs its dispatcher. A current Lefthook dispatcher with an existing `.old`
entry remains allowed.

`hooks install` resolves `smt` and `lefthook` with `exec.LookPath`, then
uses argument-array `git config --get core.hooksPath` in every initialized
configured repository before any installer mutation. Any nonempty effective
setting, including a relative one, blocks all installation as a custom hook-path
policy; resolve it manually rather than forcing or resetting it. It then
requires a regular eligible `commit-msg` target and a top-level `commit-msg`
mapping in `lefthook.yml`, and runs `lefthook validate` in every repository
using argument arrays. A symlink, directory, or other nonregular `commit-msg`
target is unmanaged and blocks the plan. It completes all root-and-child
preflight before installing anything, then runs argument-array
`lefthook install commit-msg` root-first. `--dry-run` performs that preflight
and prints the configured repository plan without changing hooks. A successful
real install prints installed repository IDs. If a later real install fails,
use its installed and pending IDs for manual recovery; SMT does not force,
reset a collision, use a shell, overwrite unmanaged hooks, or undo an earlier
install.

For an exact legacy SMT `commit-msg` hook, preflight also checks
`commit-msg.old`. If any entry exists—including a symlink—both the real install
and `--dry-run` reject the whole plan before root-first execution. Lefthook
2.1.10 would refuse this migration without `--force`; resolve the collision
manually instead. The collision error does not disclose paths or hook contents.
An existing `.old` beside a current Lefthook dispatcher is allowed.

Fixture evidence is narrow: a clean fixture installed all configured hooks and
accepted a normal commit. In a deliberate negative test, removing the
installer-provided Lefthook binary while retaining `smt` on PATH caused Git to
reject an otherwise valid commit with the assertion error. Treat this as proof
of the assertion path, not as a full human E2E result.

## Build and validate

```sh
# From the SMT source checkout.
task build                         # creates bin/smt
task verify                        # runs go test ./...
export PATH="$PWD/bin:$PATH"
smt validate-message .git/COMMIT_EDITMSG
smt validate-message --config ../platform/smt.yaml .git/COMMIT_EDITMSG
```

`validate-message FILE` expects a complete commit-message file. `--config`
selects its configuration file and is useful from a child repository hook.

## Profiles and contracts

Profiles and reusable contracts remain valid in `smt.yaml` for configuration
and diagnostics. `smt status` and `smt doctor` summarize profile names and
contract counts. The former standalone check, contract-validation, CI-audit,
and guarded-bump command surfaces have been retired; use direct Beads tickets
for work that needs follow-up.

The global flag must lead the command. It preserves machine-readable output:

```sh
bin/smt --verbose status --json > status.json
```

JSON remains on stdout in `status.json`; Logrus diagnostics go only to stderr.

Colors are enabled automatically for interactive terminals. Use `NO_COLOR` to
disable ANSI colors, or `CLICOLOR_FORCE=1` to force them when output is being
redirected; `NO_COLOR` takes precedence.

```sh
bin/smt --verbose check --profile hook
NO_COLOR=1 bin/smt --verbose check --profile hook
CLICOLOR_FORCE=1 bin/smt --verbose check --profile hook 2> verbose.log
```

## Release flow

```sh
task release:build VERSION=v0.1.0
ls dist/smt_v0.1.0_{linux,darwin}_{amd64,arm64}.tar.gz dist/checksums.txt
```

The version must be strict `vMAJOR.MINOR.PATCH`. The local build creates four
archives for Linux/macOS and amd64/arm64 plus `dist/checksums.txt`; it does not
tag or push.

```sh
# Mutating: verify the worktree and release decision before running this.
task release:tag VERSION=v0.1.0
```

`task release:tag` requires a fully clean worktree, runs verification and the
release build, creates an annotated tag, and pushes it to `origin`. The pushed
tag triggers `.github/workflows/release.yml`, which publishes a GitHub Release
with the four archives and checksum file. This command was not invoked during
implementation verification; no tag, push, or publication was performed.

## Related

- [[../00-project/SMT - Product Concept]]
- [[../00-project/SMT - Implementation Spec]]
- [[../../README|Repository README]]
