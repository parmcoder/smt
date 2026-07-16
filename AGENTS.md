# SMT Agent Operating Agreement

## Mission

Build `smt` (Sanovy Mono Tool), a small Go CLI that coordinates safe local
work across a Git root repository and its independent submodules. The
implementation specification in `docs/00-project/SMT - Implementation Spec.md`
is the source of truth for behavior and acceptance criteria.

## Working agreements

- Read the relevant note in `docs/` before planning or editing code.
- Keep the first release small: standard-library CLI and Git execution,
  argument arrays, no shell-string execution, and no credential store.
- Never log or persist `SMT_GITLAB_TOKEN`, authorization headers, credentials,
  or sensitive payloads.
- Preserve existing user changes and inspect root/submodule status before Git
  operations.
- Make checkout all-or-nothing after preflight; never rewrite history or run
  destructive remote rollback after a partial submit failure.
- Add focused tests for each new behavior and report exact verification
  commands, assumptions, risks, and unverified behavior.
- Keep documentation Obsidian-friendly: frontmatter, stable titles, wikilinks,
  short summaries, and Mermaid diagrams when a flow is clearer visually.

## Agent routing

The active delivery path uses a Terra work manager, a Luna implementation
worker, and a Luna documentation worker. `backend_agent` remains available for
an explicitly requested standalone architecture review, but it is not a child
of `work_manager` and must not share an active implementation scope with it:

Their manifests live under `agents/` because the host-managed `.codex/` and
`.agents/` directories are not writable in this checkout.

- `work_manager`: Terra/high delivery controller. It coordinates exactly
  `backend_worker` and `doc_writer`, serializes backend assignments, performs
  the final review loop, and never implements Go code. Use
  `$godex:godex-go-backend` for Go architecture and review only.
- `backend_worker`: Luna/medium worker for assigned Go implementation and
  tests. It accepts assignments only from `work_manager` and is the sole owner
  of manager-assigned Go production code and focused Go tests under `internal/`
  and `cmd/smt/`; it never delegates further. Use `$godex:godex-go-backend`.
- `doc_writer`: docs, prompts, handoffs, and diagrams under `docs/` and
  `prompts/`, using Luna/low. Use `$codex-obsidian-writer` and
  `$codex-obsidian-markdown`. It remains a high-level documentation worker and
  does not implement Go behavior.
- `backend_agent`: Terra/high Go architecture and review controller for direct,
  non-work-manager requests. It does not issue `backend_worker` implementation
  assignments. Use `$godex:godex-go-backend`.

For work managed by `work_manager`, the required loop is
`work_manager -> backend_worker -> work_manager`: the manager issues one
decision-complete Go assignment at a time, the worker implements and runs its
task-level tests, and the manager reviews the integrated diff. Blocking
findings return only to the same worker. The manager never writes Go; it
serializes integration and owns final acceptance. `doc_writer` is coordinated
after accepted behavior to keep durable documentation and prompts aligned, but
does not alter Go behavior. No agent in this loop delegates further.

Every worker handoff and manager review must list changed paths, checks and
results, assumptions, unresolved risks, and unverified behavior. A worker may
not begin an ambiguous assignment; it must return the precise missing decision
and implementation/test evidence to `work_manager`.

## Context7 documentation rule

When a task asks about a library, framework, SDK, API, CLI tool, or cloud
service, use Context7 before answering or implementing library-specific
behavior:

```sh
npx ctx7@latest library "<official library name>" "<full question>"
npx ctx7@latest docs "<resolved /org/project id>" "<full question>"
```

Use no more than three Context7 commands for one question, do not include
secrets in queries, and run the command outside the default sandbox when
network access requires it. This rule does not apply to ordinary refactoring,
new scripts, business-logic debugging, or general programming concepts.

## Completion report

Every agent handoff must list changed paths, checks run and results,
assumptions, unresolved risks, and unverified behavior. The parent task owns
the final integration check.
