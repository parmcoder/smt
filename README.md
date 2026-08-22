<div align="center">

# SMT

### Sanovy Mono Tool

Safely coordinate a Git root repository with independent submodules through
reviewable blueprints and visible lifecycle operations.

[![Status: In development](https://img.shields.io/badge/status-in%20development-yellow)](https://github.com/parmcoder/smt/blob/main/docs/superpowers/plans/2026-08-17-smt-v0.1.0-production.md)
[![Go 1.26.5](https://img.shields.io/badge/go-1.26.5-00ADD8?logo=go&logoColor=white)](https://github.com/parmcoder/smt/blob/main/go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](https://github.com/parmcoder/smt/blob/main/LICENSE)

</div>

> **Development status:** source builds are supported. APIs and generated
> starters remain evolving. `apply` creates a deterministic, Git-ready
> workspace; when Web is selected it runs the pinned local Next.js CLI with
> `--skip-install`, and when Mobile is selected it runs the pinned local Flutter
> CLI with `--no-pub`. Apply never performs dependency resolution or package
> installation.

## Why SMT

SMT makes coordination inspectable: review the blueprint, see the root and
each independent repository, and run guarded lifecycle operations with clear
preflight and recovery boundaries. It uses argument-array Git execution,
child-first pushes and pulls, root-first worktree creation, Beads-aware branch
operations, and no credential persistence.

## Available today

| Area | Current capability |
| --- | --- |
| Blueprint | `smt new` creates a reviewed configuration; `smt apply` creates the root and selected independent submodules. |
| Repository lifecycle | `push`, `remote provision`, `pull`, and synchronized `worktree add`. |
| Beads lifecycle | `prepare` and `switch` coordinate existing Beads-ID branches. |
| Diagnostics | `status` and `doctor` report repository, executable, remote, hook, and profile readiness. |
| Hooks | Guarded Lefthook installation and conventional commit validation. |
| Web | `.3.2.1` creates a Git-ready Next.js CLI baseline; `.3.2.2/.3` quality and runtime lanes remain deferred. |
| Mobile | Current Mobile output is a Git-ready Flutter CLI-generated Android/iOS starter; `.3.5.3` runtime and verification remain deferred. |

## Getting started from a fresh clone

Prerequisites: Git, Go 1.26.5, Task, and Beads `bd`. Lefthook is optional
until you install hooks. Build and verify SMT first:

```sh
git clone https://github.com/parmcoder/smt.git
cd smt
task verify
task build
./bin/smt --help
mkdir -p ../platform-config
./bin/smt new ../platform-config/smt.yaml
# Inspect and edit ../platform-config/smt.yaml.
./bin/smt apply --config ../platform-config/smt.yaml ../platform
export PATH="$PWD/bin:$PATH"
cd ../platform
smt doctor
```

`smt new` writes a blueprint only after confirmation. Inspect it before
`apply`; the destination must be new. `apply` initializes Beads metadata and
does not run `flutter pub get`, `npm install`, resolve packages, call provider
APIs, or create remote projects. Selected Web and Mobile applies require their
pinned local asdf toolchains; other components remain independent of those
prerequisites.

## Current Web in-development workflow

The accepted Web path is build-from-source through the local Next.js CLI. After
the root `.tool-versions` file is staged, `smt apply` runs this exact
argument-array command in a temporary Web directory and preserves the CLI
files:

```sh
asdf exec npx --yes create-next-app@16.2.9 <staged-web-directory> --typescript --eslint --app --empty --tailwind --use-npm --skip-install --disable-git --agents-md --import-alias=@/*
```

The root pin is `nodejs 24.18.0`. Apply merges the CLI `.gitignore`, publishes
no `package-lock.json`, runs no `npm install`, and performs no dependency
resolution. If initialization fails, the staged workspace is discarded and
the destination is not published; retry with:

```sh
asdf install nodejs 24.18.0
asdf current nodejs
asdf exec npx --yes create-next-app@16.2.9 --help
```

This pinned `npx create-next-app` invocation is the only Apply path that may
access the npm registry. Non-Web and static Apply paths remain offline; the
Web exception still performs no installation, lockfile publication, or
dependency resolution.

After Apply, work from `web-app/` with `asdf exec npm install` and
`asdf exec npm run dev`. The later `web_worker` lane owns the lockfile,
quality, browser, and runtime checks; this initializer does not claim that
those checks or a real browser/device lane have run. A selected Web Apply also
generates Web-specific `web_worker` routing and skills metadata.

## Current Mobile in-development workflow

The current Mobile path is build-from-source. After the root `.tool-versions`
file is staged, `smt apply` runs this exact command in its staged Mobile
directory and preserves the Flutter CLI output:

```sh
asdf exec flutter --suppress-analytics create --empty --no-pub --platforms=android,ios --org=com.example.smt --project-name=smt_mobile --description="A provider-neutral SMT Flutter mobile starter." <staged-mobile-directory>
```

The root pin is `flutter 3.44.9-stable`. If the pinned toolchain is missing,
Apply fails atomically before the destination is published and reports:

```sh
asdf install flutter 3.44.9-stable
asdf current flutter
```

After a successful Apply, work from `mobile-app/` with `asdf exec flutter pub
get` and `asdf exec flutter analyze`; this later Mobile-worker step produces
and verifies `pubspec.lock` and the pinned `flutter_lints 6.0.0` policy. In the
current checkout, the asdf Flutter create, pub get, and analyze checks pass.
Android device/build checks
are unverified because the Android SDK is absent; iOS checks are unverified
because Xcode is incomplete and CocoaPods is missing. Device execution and
`.3.5.3` runtime verification remain deferred. Mobile does not require local
Compose to launch, while Compose/runtime Containerfiles remain later work.

```mermaid
flowchart LR
    A["smt new"] --> B["Inspect smt.yaml"]
    B --> C["smt apply"]
    C --> D["Root + selected independent submodules"]
    D --> E["status / doctor"]
    E --> F["Lifecycle commands"]
```

## Roadmap

All roadmap items are planned unless marked as available above.

| Horizon | Planned direction |
| --- | --- |
| Now | v1 module/starter restructure around Web, Mobile, API, and Database with a Podman-first runtime. |
| Next | Manifest/toolchain Taskfiles, security, integration/runtime verification, and v0.1.0 human/release gates. |
| Later | `smt extend`, provider-specific CI, observability, managed upgrades, SBOM/signing, and cloud/platform discovery. |

## Safety principles

- Review before apply; preflight all repositories before side effects.
- Use argument arrays; never persist credentials or authorization headers.
- Never force-push, rewrite history, overwrite unmanaged hooks, or silently
  install tools and integrations.
- Report completed and pending work after partial lifecycle failures; do not
  perform destructive automatic rollback.

## Documentation

- [Documentation index](https://github.com/parmcoder/smt/blob/main/docs/README.md)
- [Implementation Spec](https://github.com/parmcoder/smt/blob/main/docs/00-project/SMT%20-%20Implementation%20Spec.md)
- [Product Concept](https://github.com/parmcoder/smt/blob/main/docs/00-project/SMT%20-%20Product%20Concept.md)
- [Command Recipes](https://github.com/parmcoder/smt/blob/main/docs/10-development/SMT%20-%20Command%20Recipes.md)
- [Component Developer Toolchains](https://github.com/parmcoder/smt/blob/main/docs/10-development/SMT%20-%20Component%20Developer%20Toolchains.md)
- [Extensible Modules Design](https://github.com/parmcoder/smt/blob/main/docs/superpowers/specs/2026-08-17-smt-extensible-modules-design.md)
- [v0.1.0 Production Plan](https://github.com/parmcoder/smt/blob/main/docs/superpowers/plans/2026-08-17-smt-v0.1.0-production.md)

## Contributing

Contributions are tracked with Beads. Start with `bd ready`, create or claim a
task before changing files, use the exact task ID as the branch name, and run
`task verify`. Read [AGENTS.md](https://github.com/parmcoder/smt/blob/main/AGENTS.md)
for the repository and agent workflow.

## License

SMT is released under the [MIT License](https://github.com/parmcoder/smt/blob/main/LICENSE).
