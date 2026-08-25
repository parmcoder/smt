---
type: implementation-spec
status: active
owner: platform
tags:
  - go
  - git
  - gitlab
  - github
  - monorepo
  - developer-experience
created: 2026-07-15
updated: 2026-08-22
---
# SMT — Sanovy Mono Tool

## Summary

`smt` is a small Go CLI for a Git root plus independent submodules.
Configuration is committed in `smt.yaml`, remains at `version: 1`, and contains
no credentials. The accepted implementation includes local workspace
scaffolding, guarded Git lifecycle operations, diagnostics, safe hooks, and
Beads-backed lifecycle support. Profiles and reusable contracts remain valid
configuration and are summarized by diagnostics; their former standalone
execution commands are not part of the public CLI.

The Cobra root help lists the retained workspace, Git, diagnostics, hooks, and
Beads lifecycle commands. Implemented commands are:

- `smt new [FILE]` — interactively select independent Web (`nextjs`), Mobile
  (`flutter`), API (`go`), and Database (`postgresql`) components. Immediately
  after the Web selection it asks `Include Flutter mobile application? [Y/n]`.
  Enter includes the Android/iOS Flutter Mobile component and an explicit no
  excludes it. After the Database selection it asks the optional,
  default-no quality-root question `Include E2E quality declaration? [y/N]`.
  An affirmative answer records `modules: [e2e]` on the root repository only;
  it does not create an E2E repository or scaffold. Component repositories
  receive their exact catalog module IDs. It writes a validated `smt.yaml`
  blueprint only after confirmation and does not create a repository or
  workspace. Blueprint generation remains byte-stable without network access
  for identical selections in fresh destinations; selected Web Apply is the documented
  pinned `npx` initializer exception and may access the npm registry without
  installing or resolving dependencies.
  Existing destination files are refused; `smt new` never overwrites, merges,
  regenerates, or upgrades a blueprint.
- `smt apply [--config FILE] PATH` — validate the supplied workspace
  blueprint/configuration, then create the root Git repository, selected local
  bootstrap submodules, ignore files, Beads metadata, and repository-local
  agent workflow files plus deterministic root `compose.yaml` and
  `.env.example` at a new destination; root `.gitignore` ignores `.env`. It
  does not install dependencies, create remote repositories, call provider
  APIs, or invoke Podman/Compose. It refuses an existing destination file or
  directory; apply has no overwrite, merge, regenerate, upgrade, or `smt extend`
  path. Generated blueprints must contain the exact repository module
  annotations from the static catalog; apply persists those annotations but
  does not execute their verification recipes, install their tools, skills, or
  MCP integrations, mutate host configuration, or create module repositories.
- When Web is selected, apply stages the root `nodejs 24.18.0` pin and runs the
  accepted local Next.js initializer described below. It publishes the
  CLI-owned baseline without `package-lock.json`, `npm install`, or dependency
  resolution; a failed staged initializer is atomic and leaves no published
  destination.
- `smt push [--dry-run]` — preflight every configured repository, then push
  each child repository's current branch before the root. Remote URLs come from
  `repositories[].remote.url`; dry-run validates and prints the order without
  contacting remotes.
- `smt remote provision [--dry-run] [--json]` — discover or create every
  configured GitHub/GitLab project child-first, then persist returned SSH URLs
  and wire local origins only after all provider targets are available.
- `smt worktree add PATH --branch NAME [--dry-run]` — preflight the root and
  every configured submodule, then create one new linked worktree branch per
  repository at the same destination layout. It rejects dirty, detached,
  uninitialized, branch-colliding, gitlink-mismatched, or existing-destination
  state.
- `smt prepare` — create and report one open `Prepared workspace` Beads task,
  then run complete preflight before creating the active Beads-ID branch in
  every configured repository. If preflight fails, the task remains open and
  Git remains mutation-free. Tracked and untracked changes are stashed;
  ignored files are left in place. The operation has no positional arguments.
- `smt switch [BEAD_ID]` — with no argument, switch every repository to its
  effective default branch; with a Beads ID, switch every repository to its
  existing local active-task branch. It never creates branches, auto-pops
  stashes, or rolls back a partial switch.
- `smt pull` — fast-forward configured repositories child-first, then root,
  using each repository's effective default branch.
- `smt hooks install [--dry-run]` — require bare `smt` and `lefthook` on
  `PATH`, then preflight every configured initialized worktree, valid
  `lefthook.yml` `commit-msg` mapping, `lefthook validate` result, and eligible
  `commit-msg` hook before installing Lefthook dispatchers root-first. Dry-run
  prints the complete plan without mutation.
- `smt validate-message [--config FILE] FILE` — validate one complete
  conventional commit message. On a non-default active Beads branch it also
  requires the exact branch ID in `type(scope): [BEAD-ID] summary`; default
  branches use ordinary configured conventional-commit syntax.
- `smt status [--json]` — inspect configured repositories and summarize
  available profiles and contract findings.
- `smt doctor [--tree]` — report repository, Beads, hook, executable, remote,
  and profile readiness without printing secret values. The default is an
  action-first summary; `--tree` renders the detailed tree. `--verbose` adds
  safe diagnostic detail only.
These commands use argument arrays, never force-push or rewrite history, and
never log or persist tokens, authorization headers, or sensitive payloads.

Agents create and manage feature or task tickets directly with Beads. The
supported ticket workflow is:

```sh
bd prime
bd create --title="Short task title" --description="Why this exists and what needs to be done" --type=task --priority=2
bd show <id>
bd update <id> --claim
bd ready
bd blocked
bd close <id> --reason="Completed"
```

Create the implementation ticket before editing code. `smt prepare` may still
create its special internal `Prepared workspace` task for repository lifecycle
coordination.

## Next.js Web component

The Web stack value is `nextjs`, with Next.js `16.2.9` on root-pinned Node.js
`24.18.0`. When Web is selected, `smt apply` stages the child outside the
published destination and invokes this exact argument-array command:

```sh
asdf exec npx --yes create-next-app@16.2.9 <staged-web-directory> --typescript --eslint --app --empty --tailwind --use-npm --skip-install --disable-git --agents-md --import-alias=@/*
```

Apply preserves the CLI-owned `package.json`, App Router, Tailwind, `AGENTS.md`,
and other generated files, merges the CLI `.gitignore` with SMT-required
entries, and publishes no `package-lock.json`. `--skip-install` means Apply
does not run `npm install` or resolve dependencies. The staged CLI output is
published only after initialization succeeds; failures preserve CLI output in
the error, report `asdf install nodejs 24.18.0`, `asdf current nodejs`, and
`asdf exec npx --yes create-next-app@16.2.9 --help`, and leave the destination
unpublished.

The pinned `npx create-next-app` invocation is the sole Apply exception that
may access the npm registry. Non-Web and static Apply paths remain offline;
Web still performs no `npm install`, lockfile publication, or dependency
resolution.

Selecting Web also generates Web-specific `web_worker` routing and a worker
manifest requiring `build-web-apps:react-best-practices` and
`build-web-apps:frontend-testing-debugging`. After Apply, the local workflow
is `asdf exec npm install` followed by `asdf exec npm run dev` from
`web-app/`; the later `.3.2.2/.3` Web worker lanes own dependency lockfile,
quality, browser, and runtime verification. `.3.2.1` claims no real npm,
browser, or runtime evidence.

## Flutter Mobile component

`smt new` asks the literal prompt `Include Flutter mobile application? [Y/n]`
immediately after the Web selection. Enter means Yes and an explicit no
excludes Mobile. When Mobile is selected, the component and repository order is
repo, web, mobile, api, database; an opt-out omits Mobile.

Mobile extends, rather than changes, `version: 1`. Existing version-1
blueprints/configurations that lack Mobile remain valid; applying one produces
no Mobile output. When selected, `smt new` emits and `smt apply` accepts
exactly this stack entry and repository mapping:

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

Only `flutter` is supported for the Mobile stack in version 1. Any unsupported
Mobile stack or mismatched Mobile metadata is a validation failure before any
destination mutation. Version 1 targets Android and iOS only; store signing,
metadata, submission, and publication are outside this workspace-setup scope.

Applying a selected Mobile blueprint creates the same Git-ready component
basics as the existing stacks: an independent initialized local bootstrap
submodule, a `mobile_worker` manifest, Flutter-oriented README and ignore
rules, and a root `.tool-versions` entry containing `flutter 3.44.9-stable`.
The `.3.5.1` contract owns the Flutter base-manifest policy: Flutter owns the
`pubspec.yaml`, `analysis_options.yaml`, and project baseline created during
Apply. The `pubspec.lock` and pinned `flutter_lints 6.0.0` policy are produced
and verified later by `mobile_worker` after `asdf exec flutter pub get`.
Because Apply uses `--no-pub`, it emits no lockfile and performs no package
resolution.

For `.3.5.2`, Apply stages the Mobile child, stages the root
`.tool-versions`, then runs this exact local command in the staged directory:

```sh
asdf exec flutter --suppress-analytics create --empty --no-pub --platforms=android,ios --org=com.example.smt --project-name=smt_mobile --description="A provider-neutral SMT Flutter mobile starter." <staged-mobile-directory>
```

Apply preserves the CLI output. It does not use static Android/iOS templates
and Go does not write app source, tests, or analysis files after the CLI
returns. `--no-pub` keeps Apply offline: it does not run `flutter pub get`,
resolve packages, access the network, sign an app, or publish an app. If the
pinned asdf/Flutter toolchain is unavailable, Apply reports:
`asdf install flutter 3.44.9-stable` and `asdf current flutter`, then fails
atomically before destination publication. The child README guides the
subsequent `asdf exec flutter pub get`, analysis, and device setup.

`smt apply` validates first and remains atomic/all-or-nothing. Any
prerequisite, staging, Beads, or publish failure leaves no partial destination.
Existing all-or-nothing semantics remain, and SMT must never attempt remote
rollback after a later submit failure.

## Configuration contract

Generated blueprints from `smt new` carry this exact top-level provenance
mapping. It identifies the generator and reviewed template set without a
timestamp, user, machine or path, Git SHA, random value, or environment-derived
field. The root `smt.yaml` uses this shape (the existing file is canonical):

```yaml
version: 1
provenance:
  tool: smt
  smt_version: v0.1.0
  template_set_version: v1
workspace:
  ai_assist: codex
  stack:
    web: nextjs
    mobile: flutter
    api: go
    database: postgresql

repositories:
  - id: repo
    path: .
    scope: repo
    modules: [e2e]
    remote:
      url: ""
  - id: web
    path: web-app
    component: web
    technology: nextjs
    scope: web
    modules: [web]
    remote:
      url: git@github.com:example/web-app.git
  - id: mobile
    path: mobile-app
    component: mobile
    technology: flutter
    scope: mobile
    modules: [mobile]
  - id: api
    path: apis
    component: api
    technology: go
    scope: api
    modules: [api]
  - id: database
    path: database
    component: database
    technology: postgresql
    scope: database
    modules: [database]

contracts:
  reference:
    - id: ci-pin
      repository: repo
      file: .gitlab-ci.yml
      expected: old
      replacement: new
      severity: warn
  migration-coverage:
    - id: migration
      repository: database
      file: migrations/001.sql
      source: docs/migration.md
      expected: delivered
  artifact:
    - id: bundle
      repository: web
      file: dist/app.js
      expected: present
```

The provenance mapping in a generated blueprint must contain exactly `tool: smt`,
`smt_version: v0.1.0`, and `template_set_version: v1`; unknown provenance fields
are rejected. `smt apply` accepts only this exact provenance for a
generated blueprint. Missing or unsupported provenance fails validation before
service or destination mutation. A general, non-generated version-1
configuration without provenance remains usable by lifecycle and diagnostic
commands, but it is not applyable as a new generated blueprint.

The supported version-1 stack values are `web: nextjs`, optional
`mobile: flutter`, `api: go`, and `database: postgresql`; `ai_assist` is either
absent or `codex`. New blueprints use the deterministic repository order
`repo`, `web`, `mobile`, `api`, `database`, omitting `mobile` when it is not
selected. They contain no DevOps prompt, `workspace.stack.devops`, combined
`infra` or DevOps repository, Docker/OpenTofu component or tooling metadata,
or generated DevOps artifacts.

Repositories may also carry optional `modules: [id...]` metadata. Omitting the
field remains valid for existing general version-1 configurations. Version 1
uses a static, schema-versioned catalog owned by the SMT code; it is not user
YAML. The implemented schema-v1 catalog contains exactly 11 declarations: five
selectable modules (`web`, `mobile`, `api`, `database`, and `e2e`) and six
non-selectable platform declarations (`container`, `cicd`, `observability`,
`iac`, `k8s`, and `argocd`).

### Implemented `.5` module declaration contract

Every declaration records its ID, selectable flag, category/layer,
provided/required/optional capabilities, safe repository placement, stable
completion-criterion IDs, agent and skill references, argument-array
verification requirements, and reviewed scaffold-asset identity where
applicable. The placement mode is declarative and must be one of `attached`,
`shared`, or `independent`; `.5` uses `attached` and `independent` in the
built-in catalog, while `shared` remains an available mode for a later
declaration. `Targets` names the module IDs or the root `repo` boundary that a
placement is attached to.

The following matrix is authoritative for the built-in catalog:

| ID | Selectable | Placement (`path`, `scope`, `mode`, `targets`) | Completion criteria | Requires |
| --- | --- | --- | --- | --- |
| `web` | yes | `web-app`, `web`, `independent`, `[web]` | `web.declaration` | — |
| `mobile` | yes | `mobile-app`, `mobile`, `independent`, `[mobile]` | `mobile.declaration` | — |
| `api` | yes | `apis`, `api`, `independent`, `[api]` | `api.declaration` | — |
| `database` | yes | `database`, `database`, `independent`, `[database]` | `database.declaration` | — |
| `e2e` | yes | `.`, `repo`, `attached`, `[repo]` | `e2e.declaration` | — |
| `container` | no | `.`, `repo`, `attached`, `[web, api]` | `container.declaration` | — |
| `cicd` | no | `.`, `repo`, `attached`, `[repo, web, mobile, api, database]` | `cicd.repository-boundary` | — |
| `observability` | no | `.`, `repo`, `attached`, `[web, api, database]` | `observability.boundary` | — |
| `iac` | no | `platform/iac`, `iac`, `independent`, `[repo]` | `iac.provider-neutral` | — |
| `k8s` | no | `platform/k8s`, `k8s`, `independent`, `[repo]` | `k8s.static-validation` | — |
| `argocd` | no | `platform/argocd`, `argocd`, `independent`, `[repo]` | `argocd.sync-policy` | `k8s` |

Web, Mobile, and API use the `application`/`application-components` pairing;
Database uses `infrastructure`/`shared-infrastructure`; E2E uses
`quality`/`quality-verification`; and all six platform declarations use
`platform`/`platform-delivery`. Completion criteria are stable declarative IDs,
not commands that SMT executes. `argocd` requires the `k8s` capability.

Catalog validation rejects an unsupported schema, duplicate IDs or declaration
references, invalid category/layer pairs, unknown module or capability
references, unsafe paths, invalid placement modes/targets, duplicate
completion criteria, and dependency cycles. Configuration validation rejects
unknown or duplicate repository module IDs and missing selected required
capabilities. `Config.LoadBytes` accepts known non-selectable platform metadata
when its references and dependencies are valid (for example, `[argocd, k8s]`),
but `Apply` and `ValidateBlueprint` reject any non-selectable platform metadata
before topology checks, staging, or destination mutation.

`smt new` derives the quality prompt and emitted module ID from the catalog's
role and placement. Component repositories receive exact selectable IDs
(`web`, `mobile`, `api`, and `database`). Opting into the quality declaration
adds only `modules: [e2e]` to the root blueprint. Current Apply remains
metadata-only for E2E; the P0 `smt-4xf.14` rollup will add separate attached
`e2e/web` and `e2e/mobile` packages when their matching components are
selected. With no Web or Mobile target, the declaration remains valid and
emits no runnable package. The planned packages use Playwright for Web and
Flutter's native `integration_test` for Mobile, with contract smoke only:
stable navigation hooks, Web `/healthz`, optional API reachability, and Mobile
launch plus `mobile-home`/`api-status` keys. Apply will not install packages,
browsers, SDKs, devices, credentials, or remote CI; local tasks delegate to
existing component startup commands. The `.5` slice still creates no
platform repositories or platform runtime artifacts, and `smt extend` remains
deferred.

### Implemented `.3.1` local OCI runtime contract

Applying a blueprint writes deterministic root `compose.yaml` and
`.env.example` files and adds `.env` to the root `.gitignore`. The renderer is
offline and declarative. Compose service IDs are only `web`, `api`, and
`database`; Mobile remains outside OCI Compose. The valid API-only,
Database-only, Web-only, API+Database, all-OCI, empty, and Mobile-only
selections remain valid, and Compose emits only the selected OCI services. An
empty or Mobile-only selection emits `services: {}`.

The default host bindings are `3000:3000` for Web, `8080:8080` for API, and
`5432:5432` for Database. The canonical overrides are `WEB_PORT`, `API_PORT`,
and `DATABASE_PORT`; zero uses the reviewed default and a non-zero override
must be a TCP port from 1 through 65535. When an override is unset, the
generated `.env.example` derives a deterministic workspace-scoped host port so
fresh workspaces can run in parallel; explicit overrides remain authoritative.
The Compose project name is derived only from the destination basename:
lowercase, safe hyphen form, capped at 63 characters, with `smt-workspace` as
the fallback. Generated local resource names are scoped by that project as
`<project>-postgres-data`, `<project>-zitadel`, and
`<project>-zitadel-bootstrap`, while explicit resource overrides remain
supported. The same values appear in `.env.example`, which contains examples
only and the local-development `DATABASE_PASSWORD=smt-dev-password` value.
Replace it outside disposable local use. Its declarative examples include `COMPOSE_PROJECT_NAME`,
the three port overrides, `DATABASE_VOLUME`, `API_BASE_URL`, `DATABASE_HOST`,
`DATABASE_NAME`, and `DATABASE_USER`; no production or secret credentials or
`.env` file is generated.

When the optional identity module is selected, the generated proxy always uses
Podman. Traefik retains its Docker-compatible provider because that provider
speaks the Podman API, but its endpoint is explicitly
`unix:///var/run/podman/podman.sock`; the host socket is supplied through
`PODMAN_SOCKET` and never falls back to `/var/run/docker.sock`. The generated
example assumes the rootless Linux socket
`/run/user/1000/podman/podman.sock`; operators must change it for another UID,
rootful Podman (`/run/podman/podman.sock`), or their local Podman machine and
enable the Podman API socket before `task compose:up`.

Web's contract probes `/healthz`. API health/readiness are `/healthz` and
`/readyz`, and Database health uses `pg_isready`. When both services are
selected, Web depends on API with `condition: service_healthy`; when API and
Database are selected, API depends on Database with the same condition. The
generated `.3.1` contract may reference component build contexts at
`./web-app`, `./apis`, and `./database`. `.3.1` does not generate Web/API
Containerfiles or add application-domain behavior; Database `.3.4.1` now owns
the independent PostgreSQL Containerfile, Taskfile, and readiness assets.

The pure `runtime.Preflight` API validates override ranges and selected-port
collisions, can report occupied ports through an injected port check, and can
report missing Podman or Podman Compose through injected prerequisite checks.
 Its errors identify the service/port or environment key and an actionable
 change/install/configuration step. It does not execute external commands by
 itself. `smt apply` renders the contract and, for selected Web, invokes only
 the staged pinned `npx create-next-app` initializer, which is the one allowed
 registry-access exception; for selected Mobile, it invokes only the staged
 local Flutter `create --empty --no-pub` command above. It does not invoke
 Preflight, Podman, Compose, socket probing, runtime health checks, `npm
install`, `flutter pub get`, or package resolution. OCI-selected workspaces also
receive root Compose Taskfile entrypoints that pass the operator-managed root
`.env` explicitly and fail early when it is missing (or when a selected
Database has no password). Remaining Web dependency, quality, browser, and
runtime lanes, Database lifecycle verification, Mobile platform
SDK/device verification, and component lifecycle tasks remain later work. The
broader root and Database task aggregation belongs to `smt-4xf.6.1.2` and
later.

### Implemented `.3.3.1` API module manifests

When API is selected, the generated API child repository receives deterministic
static `go.mod` and `go.sum` files. The module is `example.com/smt/apis` and
the language line is `go 1.26.5`. API-only requires Huma
`github.com/danielgtaylor/huma/v2 v2.39.1`. API+Database additionally requires
pgx `github.com/jackc/pgx/v5 v5.10.0` and golang-migrate
`github.com/golang-migrate/migrate/v4 v4.19.1`. Without API selection, no API
child repository and no API manifests are emitted.

The Go `tool` block pins govulncheck
`golang.org/x/vuln/cmd/govulncheck` with `golang.org/x/vuln v1.7.0` and
golangci-lint
`github.com/golangci/golangci-lint/v2/cmd/golangci-lint` with
`github.com/golangci/golangci-lint/v2 v2.12.2`. API+Database additionally pins
the migrate tool `github.com/golang-migrate/migrate/v4/cmd/migrate`. Direct
pinned checksums for these emitted modules and tool backings are present in
`go.sum`; the full transitive checksum closure is not asserted by this slice.

Apply writes these static templates only. It performs no `go`, `go mod`,
package-manager, network, or tool installation work; PATH-empty focused tests
cover that boundary. `gofmt`, `go vet`, `go test`, race, coverage, fuzzing,
Godex, and gopls are SDK/editor/agent tools, not module dependencies.
`go mod tidy` remains a later check against the eventual source closure. The
generated child `go mod verify` task is covered by the Task v3.52.0 harness;
that evidence is scoped to the emitted manifest. API source imports, Huma/OpenAPI
generation, tests, Containerfiles, and runtime verification are not claims of
the manifest slice.

### Implemented `.3.3.2` API runtime and OpenAPI assets

When API is selected, `smt apply` emits deterministic embedded assets in the
`apis` child repository: `main.go`, `internal/server/server.go`,
`cmd/openapi/main.go`, `.env.example`, `openapi.yaml`, and `Taskfile.yml`, alongside the
existing `.3.3.1` module manifests and bootstrap files. No API selection emits
none of these API assets. The generated module remains
`example.com/smt/apis` on Go `1.26.5`. Its runtime uses Huma v2.39.1 through
the Gin adapter, Gin v1.12.0, Prometheus `github.com/prometheus/client_golang v1.24.1`, and the
already-pinned Go tools. API-only generated source contains no pgx, migrate, or
database code; API+Database retains pgx and golang-migrate as manifest-only
dependencies and still emits the same API source without database behavior.

The generated `.env.example` records the runtime defaults: `HTTP_ADDR=:8080`,
`APP_ENV=development`, `LOG_LEVEL=info`, `HTTP_READ_TIMEOUT=15s`,
`HTTP_READ_HEADER_TIMEOUT=5s`, `HTTP_WRITE_TIMEOUT=15s`,
`HTTP_IDLE_TIMEOUT=60s`, `HTTP_MAX_HEADER_BYTES=1048576`, and
`HTTP_SHUTDOWN_TIMEOUT=10s`. The shared Huma API declares OpenAPI 3.1 metadata
title `SMT API` and version `v0.1.0`, with `/docs`, `/openapi.json`, and
`/openapi.yaml` routes. `cmd/openapi` constructs that same API and writes
`api.OpenAPI().YAML()` offline to stdout without starting a listener. The
committed `openapi.yaml` is byte-identical to regeneration across fresh
`Apply` destinations.

The runtime returns HTTP 200 with `status: ok` from `/healthz`. `/readyz`
returns `ready` after bootstrap and `not_ready` with HTTP 503 before bootstrap
or during shutdown. `/metrics` uses the same listener and exposes Go/process
metrics plus bounded request counters, duration, and in-flight metrics. Safe
`X-Request-ID` values are accepted or generated and returned. Custom Gin panic
recovery logs the panic, stack, route, method, and request ID through JSON
`slog`, then returns a generic HTTP 500. SIGINT/SIGTERM marks the service not
ready and performs graceful shutdown with the configured timeout.

The generated server `Config` carries direct `github.com/caarlos0/env/v11
v11.4.1` `env`/`envDefault` tags on its typed fields, including `slog.Level`
for `LogLevel` and `time.Duration` for the timeout fields. `LoadConfig()` calls
plain `env.Parse(&cfg)`; native caarlos/TextUnmarshaler parsing controls
malformed-value errors; no separate semantic conversion or post-parse
validation is added. `Run` logs a structured `configuration load failed`
event and panics with the native parse error before constructing the
application. Normal Gin/Huma runtime and graceful-shutdown behavior is
unchanged. The exact pin and direct checksums are present in the static
API-only and API+Database manifests.

The API child also receives deterministic `Taskfile.yml` with top-level
`dotenv: ['.env']`; Task does not copy or mutate `.env`. Its tasks are `build`
(`mkdir -p bin && go build -trimpath -o bin/apis .`), `run` (depends on
`build`, then runs `./bin/apis`), `test`, `coverage`, `mod` (`go mod verify`),
`openapi` (offline `GOPROXY=off GOSUMDB=off` generation compared byte-for-byte
with `openapi.yaml`), and `verify` (depends on `build`, `test`, `mod`, and
`openapi`, then runs `go vet ./...`). API+Database additionally receives
conditional API-owned `migrate:create`, `migrate:up`, `migrate:version`, and
`migrate:validate` tasks plus the neutral baseline assets; readiness and live
database lifecycle tasks remain later. The generated Task CLI harness was verified
with Task v3.52.0, including dotenv-driven `/healthz` and bounded process
cleanup.

API-selected Apply writes embedded deterministic assets only: it performs no
network, Go or package-manager command, tool installation, Task execution, Podman invocation,
listener start, or runtime execution. This slice adds no credentials, domain
CRUD, database connectivity/readiness, or root Taskfile changes,
Containerfiles, non-root packaging, or `smt extend`. Durable unit/race/fuzz/integration coverage is
`.3.3.3`; non-root packaging and runtime verification are `.3.3.4`. Later
human and Podman gates remain required. `go mod tidy` and human E2E remain
unverified; `go mod verify` evidence is limited to the generated child Task
harness and accepted focused implementation tests.

### Implemented `.3.4.2` API-owned migrations

When API and Database are both selected, Apply emits deterministic
`migrations/000001_baseline.up.sql` and `.down.sql` files containing only
`SELECT 1;`, a `scripts/validate-migrations.sh` helper, and a blank
operator-provided `DATABASE_URL=` entry in the API `.env.example`. The generated
API Taskfile adds `migrate:create NAME=...` using the pinned
`go tool migrate create -ext sql -dir migrations -seq NAME` shape,
`migrate:up`, `migrate:version`, and `migrate:validate`; validation runs up and
then version and preserves native failures without rollback. These tasks are
explicit and are never dependencies of `verify`.

API-only and Database-only selections emit no migration assets or migration
tasks. Apply remains offline: it does not invoke Go, Task, migrations,
PostgreSQL, Podman, or database provisioning. `DATABASE_URL` is an explicit
operator contract; real PostgreSQL/Podman lifecycle verification belongs to
`.3.4.3`. No automatic down, drop, force, startup migration, credentials, or
root orchestration is generated.

Legacy DevOps-shaped configurations are rejected by `smt apply` before any
destination mutation. The migration-oriented error directs the operator to
remove the legacy DevOps entries and regenerate a version-1 blueprint. The
configuration and generation behavior above describes the current
implementation; the planned runnable starter and platform work is
recorded in [[../superpowers/specs/2026-08-17-smt-extensible-modules-design|SMT Extensible Modules Design]].
The planned component gates and optional tool integrations are summarized in
[[../10-development/SMT - Component Developer Toolchains|Component Developer Toolchains]].
`remote.url` is optional at initialization but required by `smt push`; it may
not contain embedded credentials. A repository
used by `smt remote provision` must declare `provider` and a fully qualified
`project` (`owner/repository` for GitHub, or `namespace/repository` for
GitLab). `visibility` accepts `private` or `public` and defaults to `private`.
Existing provider and project metadata remain optional for local-only
repositories.

Repositories may define `hook`, `submit`, and `ci-parity` profiles. A legacy
check list is accepted for compatibility, but new configuration should use
named profiles. Each check declares `kind`, non-empty `argv`, and, when
applicable, `mutates_worktree`; mutation is never inferred from the command.
Supported reusable contracts are literal `reference`, `migration-coverage`,
and `artifact` contracts. Paths must remain inside the workspace, IDs must be
unique, and contract severity is `error` unless explicitly set to `warn`.

Applied workspaces use local bootstrap URLs in `.gitmodules` because no remote
URL is required during blueprint application. `smt remote provision` replaces
those child entries with returned SSH clone URLs and updates each repository's
`origin` plus `remote.url` in one post-discovery wiring phase. It refuses
occupied or incompatible local remotes and never deletes provider projects.

Reference and contract schemas remain declarative configuration. Diagnostics
summarize configured profiles and contract counts, but SMT does not expose the
former standalone check, audit, or guarded-bump commands.

## Explicitly planned, not implemented

The following remain approved future behavior and must not be represented as
implemented by the CLI or this release:

- changesets, release plan, and release run;
- cloud or database actions, deployment, rollback, or provider-native job
  execution;
- YAML selector rewrites or automatic CI configuration edits;
- `checkout` and `validate-range` workflows from the earlier design.

The broader runnable-starter and platform work is planned, not implemented:
the five layers remain in this repository initially, and the six platform
capabilities above are declarations only. This release still provides no Web
runtime starter or Web component Containerfile, while Database `.3.4.1`
provides its independent PostgreSQL runtime/readiness starter. Platform
repositories or scaffolds, Podman/Compose execution, Kubernetes/ArgoCD/OpenTofu runtime,
remote module registry, or `smt extend` command. Web `.3.2.1` is the implemented
CLI-owned Next.js baseline; its dependency lockfile, quality, browser, and
runtime lanes remain `.3.2.2/.3` work. Mobile `.3.5.2` is a Flutter
CLI-generated Android/iOS starter, and `.3.5.3` adds the stable app/test
contract plus the local verification lane. Platform SDK/device execution,
signing, API integration, and store publication remain outside Apply; the
host's unavailable Android/iOS targets and debug-build prerequisites are
reported as explicit unverified lanes. The
`.3.1` `compose.yaml` and `.env.example` are contract-only root artifacts,
while the `.3.3.2` API source/OpenAPI assets are deterministic offline starter
assets without packaging or lifecycle tasks. The static module catalog and
repository annotations above are implemented metadata; no platform runtime
artifact or E2E/module repository is generated. AWS + Apptainer + OpenTofu
remains later discovery.

Git lifecycle operations preflight all configured repositories before a remote
push or worktree creation. Pushes are child-first and stop after a failure with
the successful and pending repository IDs reported; no remote rollback occurs.
Worktree creation is root-first and stops with its created and pending paths
reported if a later child fails. Fixed untracked OS metadata (`.DS_Store`,
`Thumbs.db`, and `desktop.ini`) is ignored, while tracked changes remain
blocking. Provider projects and runtime tokens remain explicit
configuration/environment inputs.

## Workspace lifecycle, diagnostics, and hooks

The effective default branch is `repositories[].remote.default_branch` when
set, otherwise `main`. `prepare` and explicit-ID `switch` use Beads readiness
from the root workspace; no-argument `switch` returns to each repository's
effective default without a Beads task lookup. The active Beads ID is the
branch name, and hooks require the workspace to be ready before accepting a
commit. Commit subjects on an active Beads branch use
`type(scope): [BEAD-ID] summary`; outside one, use the normal configured
conventional-commit syntax. The root may use the active Beads ID;
there are no Jira aliases, assignment waves, manifests, or provider review
automation in this release.

`status` is a human report: it starts with `STATUS: OK`, `WARN`, or `ERROR`,
then shows each configured repository's path, Git state, branch, and
`commit-msg` hook state, followed by configured profile names, contract counts,
and safe next steps. `status --json` is the machine-readable alternative; it
returns repository entries plus profile names and contract error/warning
counts. When no profiles are configured, the human report writes
`profiles: none`; JSON remains `profiles: []`. Diagnostics are state-oriented
and do not print token values, command arguments, or command output.

Hook state is local to each configured repository: `absent` means no
`commit-msg` hook is present; `current` means the hook exactly matches a
recognized historical SMT script or the reviewed Lefthook 2.1.10 dispatcher;
`unmanaged` means another, lookalike, modified, symlinked, directory, or other
nonregular hook target exists. SMT neither follows nor overwrites unmanaged
hooks. Resolve those targets manually before running `smt hooks install`; no
`--force` or chaining mode exists. An exact recognized historical SMT hook is
eligible for migration only when no `commit-msg.old` entry exists. Lefthook
2.1.10 may preserve it as `commit-msg.old` during dispatcher installation. A
current Lefthook dispatcher with an existing `.old` entry remains allowed.

`doctor` is also read-only. It always checks Git, bare `smt`, and Lefthook, and
renders a deterministic repository-first tree. Each configured repository is
expanded in configuration order with `worktree`, `hook`, `remote`, and
`provider` children; `tools` and `credentials` are separate roots. A missing
`smt` or Lefthook is an error and produces an error exit; its remediation
directs the user to return to the SMT source checkout, build the CLI, and
expose `$PWD/bin` on `PATH`, or install Lefthook and rerun `smt doctor`, before
attempting hook installation. An absent hook or unset remote is a warning.
Provider absence is an acceptable local-only state, and configured provider
projects are summarized without URLs, tokens, command output, or private
inspection errors. Missing provider tokens are warnings because Git pushes and
manual PR/MR handoff remain available. Explanations and exact remediation are
printed below the affected warning/error node; no global duplicate next-step
list is emitted. `READY`, `WARN`, and `ERROR` remain the overall exit classes.

`smt hooks install` plans only after every configured repository passes its
preflight, so a failing child prevents all installation. It resolves `smt` and
`lefthook` with `exec.LookPath`. Before any installer mutation, it runs
argument-array `git config --get core.hooksPath` in every initialized configured
repository; any nonempty effective setting, including a relative path, blocks
the entire plan as a manually resolved custom hook-path policy. It then checks
each repository is an initialized Git worktree with a top-level `commit-msg`
mapping in `lefthook.yml`, and treats symlink, directory, and other nonregular
`commit-msg` targets as unmanaged blockers. It uses argument-array execution
for `lefthook validate` in every repository and, only after successful
preflight, for root-first `lefthook install commit-msg`.

Unmanaged custom, lookalike, modified, and nonregular hooks are never followed
or overwritten. For every exact legacy SMT `commit-msg` hook, SMT also
preflights `commit-msg.old`: if any entry exists, including a symlink, both
`smt hooks install` and `--dry-run` reject the entire plan before root-first
execution. Lefthook 2.1.10 itself refuses that migration without `--force`; SMT
requires manual collision resolution instead. The exact legacy hook is eligible
only when `.old` is absent, at which point Lefthook may preserve it as
`commit-msg.old` while installing the dispatcher. A current Lefthook dispatcher
with an existing `.old` entry remains allowed. Collision errors do not disclose
paths or hook contents. This does not authorize `--force`, shell execution,
resetting a collision, overwriting unmanaged hooks, or rollback of an earlier
install. Successful installation prints the installed repository IDs. If an
unexpected later install fails, SMT reports installed and pending IDs for manual
recovery.

Generated workspaces include root and child `lefthook.yml` files with
top-level `no_auto_install: true` and `assert_lefthook_installed: true`, plus a
`commit-msg` command that invokes bare
`smt validate-message --config FILE {1}` using the correct relative path to
the root configuration. `no_auto_install` prevents Lefthook from automatically
installing or updating hooks when configuration changes. The Lefthook assertion
makes Git fail the hook if Lefthook cannot be found, preventing a silent skip
of `smt validate-message`. In the SMT source checkout, `task build` creates
`bin/smt` but does not put it on `PATH`; use `export PATH="$PWD/bin:$PATH"`
there, then return to the target workspace for `smt doctor` or hook
installation. Both `smt` and Lefthook must remain durably available on PATH for
every hook-running environment, including IDE or GUI launches. Workspace
creation writes that scaffold only; it does not run Lefthook or install a hook.

Fixture evidence is bounded rather than universal: a clean fixture installed
the dispatcher in every configured repository and accepted a normal commit.
After deliberately removing the installer-provided Lefthook binary while
leaving `smt` on PATH, Git rejected an otherwise valid commit with Lefthook's
assertion error. This demonstrates the no-silent-skip boundary, not a completed
human end-to-end review across every launch environment.

<!-- INACTIVE: Prepared feature workspaces use `.smt/runs/<feature-id>.json` as an ignored,
secret-free snapshot of the feature, base branches/commits, repository paths,
ownership boundaries, check-profile names, integration gates, and assigned
work-item context. The file is written only after the root and child linked
worktrees are created successfully. A prepared child accepts only its assigned
Beads IDs or Jira-shaped aliases. The root accepts the feature ID and its
assigned root-task references for integration/gitlink commits. Missing,
malformed, cross-repository, ambiguous, corrupt, or out-of-date manifests fail
closed; outside a prepared workspace the existing conventional-commit behavior
is unchanged. Dry-run preparation performs no Git, Beads, or manifest write. -->

<!-- INACTIVE historical provider submission design.
## Provider remotes and workspace submission

Provider tokens are environment-only: `SMT_GITHUB_TOKEN` and
`SMT_GITLAB_TOKEN`. Provider API bases use the configured GitHub Enterprise or
self-managed GitLab endpoint, with public GitHub/GitLab defaults otherwise.
Provisioning checks all tokens, initialized clean repositories, exact project
identities, and local origin conflicts before creating anything. Missing empty
projects are created child-first; compatible existing projects are reused.
Provider-created state is never deleted as rollback. A provider partial failure
reports created, existing, and pending IDs for a later idempotent rerun.

Submission reads only the prepared manifest. It requires clean attached
worktrees, configured matching origins, available recorded target branches,
valid assigned commit references, and passing `submit` profiles before the
first push. Changed children require a root gitlink commit. Children push before
the root without force or history rewriting. Existing open PRs/MRs with the
same source and target are reused; otherwise drafts are created, with
`--ready` promoting or creating ready reviews.

When a provider token is missing, Git pushes still complete and SMT prints a
provider creation link plus copy-ready title and body. Root reviews are
deferred until selected child review URLs exist. Provider errors report
completed and pending progress without remote rollback. Real GitHub/GitLab
provisioning and mixed-provider review acceptance remain the human-owned
`smt-5w0.13.6` release gate.

## Traceability and provider release gates

The agent-owned implementation is covered by focused tests, full Go gates,
and the command recipes. Final acceptance still requires two human reviews in
disposable workspaces:

- `smt-5w0.11.7` must verify the prepared manifest's base branch, repository
  ownership, assigned Beads/Jira references, dry-run immutability, rejection
  of missing/arbitrary/cross-repository IDs, valid child commits, and the root
  feature integration commit. Corrupt, missing, ambiguous, or stale manifest
  state must fail closed. Agents must not close this ticket.
- `smt-5w0.13.6` must verify real mixed-provider project creation or exact
  reuse, SSH wiring, child-first/root-last submission, missing-token handoff,
  draft and ready review behavior, idempotent review reuse, `Closes` lines,
  child links in the root review, and secret-safe failures. Agents must not
  close this ticket.

The exact commands and evidence expectations are in the
[[../10-development/SMT - Command Recipes#Traceable workspace release-gate handoff]]
section. Record failures as linked Beads bugs and leave the corresponding
review and parent feature open.

## Local requirements and verification -->

## Local requirements and verification

Go 1.26.4 or newer and `git` are required. The SMT source checkout's
`Taskfile.yml` is the repeatable entrypoint: `task build` creates `bin/smt` and
`task verify` runs the Go tests. The implementation must keep focused tests for
configuration, scaffolded Git submodules, push/worktree preflight and recovery
reporting, hook preflight/installation, custom hook-path, nonregular-hook, and
migration/collision behavior, harmless metadata, status/doctor output,
profile/contract summaries, and secret redaction. Standalone check, CI-audit,
and guarded-bump command tests are not part of the public CLI.

The Mobile focused-test contract covers default inclusion, explicit
opt-out, invalid-answer retry, EOF/decline no-write, exact YAML/repository
mapping/scopes/order, invalid stack or metadata rejection before mutation,
existing-version-1 compatibility, atomic cleanup for preflight and
stage/publish failures, staged Flutter CLI invocation/output preservation, and
the pinned-toolchain failure guidance. Human end-to-end confirmation is later
human-owned work (`smt-3r2.5`), not completed runtime proof in this delivery.

## Flutter Mobile delivery order

The contract, blueprint, atomic apply, and documentation/release verification
work are complete through `smt-3r2.4`. The human-owned E2E review remains
`smt-3r2.5`.

## Human E2E review handoff

`smt-3r2.5` should run `smt new` twice in clean temporary locations: first
press Enter at the Mobile prompt and confirm `mobile: flutter`, the Mobile
repository entry, and the Mobile-selected `repo`, `web`, `mobile`, `api`,
`database` order; then explicitly answer no and confirm no Mobile
configuration. Apply
each reviewed blueprint to a new destination. For the default case, inspect
the Git-ready `mobile-app` submodule, `agents/mobile_worker.toml`, Mobile
README and ignore rules, root `.tool-versions` Flutter `3.44.9-stable` pin, and
the staged Flutter CLI output. Confirm the exact `asdf exec flutter
--suppress-analytics create --empty --no-pub --platforms=android,ios
--org=com.example.smt --project-name=smt_mobile
--description="A provider-neutral SMT Flutter mobile starter."
<staged-mobile-directory>` invocation is preserved. Run
`asdf install flutter 3.44.9-stable`, `asdf current flutter`, `asdf exec flutter pub get`, and
`asdf exec flutter analyze`; if Android or iOS SDK/device lanes are unavailable,
record them explicitly rather than silently skipping them. Do not claim signing
or store publication. At one additional fresh destination,
exercise one safe prerequisite, staging, Beads, or publish failure and verify
that no partial destination remains.

## Related

- [[SMT - Product Concept]] — compact product framing.
- [[SMT - Agent Team]] — ownership and documentation workflow.
- [[../../prompts/smt-build|SMT build prompt]] — execution handoff.
- [[../../AGENTS|Repository operating agreement]].
