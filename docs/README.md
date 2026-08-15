---
type: docs-index
status: active
owner: platform
tags:
  - smt
  - documentation
  - obsidian
created: 2026-07-15
updated: 2026-07-16
---
# SMT Documentation

The shared, Obsidian-compatible knowledge base for Sanovy Mono Tool.

## Start here

- [[00-project/SMT - Implementation Spec]] — canonical behavior and acceptance criteria.
- [[00-project/SMT - Product Concept]] — users, value proposition, and boundaries.
- [[10-development/SMT - Command Recipes]] — concrete CLI, check, CI, and release examples.
- [[00-project/SMT - Agent Team]] — ownership and handoff rules.
- [[../prompts/smt-build|SMT build prompt]] — implementation handoff prompt.

## Current status

The first CLI implementation is present. `task build` produces `bin/smt`,
`task verify` runs the Go suite, and `smt doctor` reports a deterministic,
repository-first readiness tree with local remediation. Provider project
provisioning and prepared-workspace submission are available through explicit
child-first commands with token-safe human handoff. GitHub Actions
tag-driven release publication is configured; local tagging remains an
explicit, mutating operation. See the recipes note for prerequisites and safe
examples.

## Documentation rules

- Keep durable decisions, contracts, and handoffs here.
- Technical notes use YAML frontmatter with `type`, `status`, `owner`, `tags`, `created`, and `updated`.
- Prefer wikilinks for vault notes and Mermaid for flows where it clarifies behavior.
