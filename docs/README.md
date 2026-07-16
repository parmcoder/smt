---
type: docs-index
status: active
owner: platform
tags:
  - smt
  - documentation
  - obsidian
created: 2026-07-15
updated: 2026-07-15
---
# SMT Documentation

This directory is the shared, Obsidian-compatible knowledge base for Sanovy
Mono Tool. It is Markdown-first and remains useful without an Obsidian app.

## Start here

- [[00-project/SMT - Implementation Spec]] — canonical product and engineering
  contract.
- [[00-project/SMT - Product Concept]] — target users, value proposition, and
  branch-to-review workflow.
- [[00-project/SMT - Agent Team]] — ownership, delegation, and handoff rules.
- [[../prompts/smt-build|SMT build prompt]] — copy/paste implementation prompt
  for the next execution task.

## Documentation rules

- Put durable decisions, contracts, and handoffs here rather than leaving them
  only in chat.
- Add YAML frontmatter with `type`, `status`, `owner`, `tags`, `created`, and
  `updated` for technical notes.
- Prefer wikilinks for notes in this vault and ordinary Markdown links for
  external references.
- Use Mermaid inside Markdown for sequences, state transitions, and ownership
  flows. Create Canvas or Bases artifacts only when spatial layout or a
  queryable index is specifically needed.
- Update the canonical spec when implementation changes the contract; do not
  create competing copies of the same requirement.

## Current status

The repository is at the docs-first scaffold stage. Go implementation is not
started. The implementation prompt and agent boundaries are ready for review
before code work begins.

## Related

- [[00-project/SMT - Implementation Spec]]
- [[00-project/SMT - Product Concept]]
- [[00-project/SMT - Agent Team]]
