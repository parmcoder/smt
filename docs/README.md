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
- [[10-development/SMT - Component Developer Toolchains]] — planned component
  toolchain, Taskfile, skill, and optional MCP contract.
- [[../prompts/smt-build|SMT build prompt]] — implementation handoff prompt.

## Current status

The first CLI implementation is present. `task build` produces `bin/smt`,
`task verify` runs the Go suite, and `smt doctor` reports a deterministic,
repository-first readiness tree with local remediation. Provider project
provisioning remains an explicit local command; workspace prepare/submit and
provider review automation are not active. GitHub Actions
tag-driven release publication is configured; local tagging remains an
explicit, mutating operation. See the recipes note for prerequisites and safe
examples.

## Documentation rules

- Keep durable decisions, contracts, and handoffs here.
- Technical notes use YAML frontmatter with `type`, `status`, `owner`, `tags`, `created`, and `updated`.
- Prefer wikilinks for vault notes and Mermaid for flows where it clarifies behavior.
