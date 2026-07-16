# smt

Sanovy Mono Tool is a planned Go CLI for safe, repeatable work across a Git
root repository and independent submodules.

## Docs-first scaffold

The project contract and execution prompts are ready before implementation:

- [Implementation spec](docs/00-project/SMT%20-%20Implementation%20Spec.md)
- [Agent team contract](docs/00-project/SMT%20-%20Agent%20Team.md)
- [Build prompt](prompts/smt-build.md)
- [Repository agent agreement](AGENTS.md)

The repository currently contains documentation, agent routing, and the
committed workspace configuration. Go implementation is the next phase.

## Planned bootstrap

The repository uses Taskfile for repeatable development commands and Lefthook
for the `commit-msg` hook. Lefthook delegates validation to the canonical SMT
validator, so local hooks and CI share the same Conventional Commit policy.

Prerequisites are `go`, `task`, and `lefthook`. Once the implementation phase
begins:

```sh
task build
task setup
```

Useful commands:

```sh
task verify
task hooks:install
task commit:validate -- .git/COMMIT_EDITMSG
```

The hook receives Git's commit-message file as `{1}` and runs
`bin/smt validate-message`. Until the Go CLI exists, `task setup` correctly
refuses to install an incomplete hook. The future `smt hooks install` command
will extend managed hooks across initialized submodules.

Submit operations use the provider configured per repository. GitLab requires
`SMT_GITLAB_TOKEN`; GitHub requires `SMT_GITHUB_TOKEN`. Neither token may be
stored in the repository or printed in command output.
