---
type: implementation-plan
status: active
owner: platform
tags:
  - smt
  - readme
  - documentation
created: 2026-08-17
updated: 2026-08-17
---
# Public README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Goal

Replace the root README with a concise, honest visitor landing page that
gets a fresh clone to a verified local build and points to canonical detail.

## Architecture

The README is a documentation-only integration surface: current CLI behavior
comes from the implementation spec and command recipes; planned work comes
from the module design and production plan; safety guidance remains linked,
not duplicated.

## Tech stack

Markdown rendered by GitHub, with one Mermaid workflow diagram and standard
Markdown links using percent-encoded paths.

## Global constraints

- Preserve current-versus-planned truth boundaries.
- Do not claim package installation, CI status, or unimplemented commands.
- Keep the README below 220 lines and omit decorative filler.
- Do not invent code tests for this documentation-only change.

---

### Task 1: Modernize the public README

**Files**

- Modify: `README.md`
- Create: `docs/superpowers/specs/2026-08-17-public-readme-design.md`
- Create: `docs/superpowers/plans/2026-08-17-public-readme.md`

**Interfaces**

- Consumes: current command behavior from `docs/00-project/SMT - Implementation Spec.md`, onboarding examples from `docs/10-development/SMT - Command Recipes.md`, and planned roadmap context from the module design and production plan.
- Produces: a GitHub-rendered visitor README with current capability claims, planned roadmap links, one Mermaid workflow, and source-first onboarding; no code or runtime behavior changes.

**Steps**

- [x] Write the README hero, honest badges, development-status callout, current capability table, source-first onboarding, workflow diagram, planned roadmap, safety principles, canonical links, contributing guidance, and license.
- [x] Keep current and planned behavior explicitly separated; do not claim package installation, CI status, or unimplemented commands.
- [x] Run `git diff --check`; expect no whitespace errors.
- [x] Run `rg -n "TBD|TODO" README.md docs/superpowers/specs/2026-08-17-public-readme-design.md`; expect no placeholder matches.
- [x] Verify all README links resolve to existing repository paths and inspect the Mermaid flow for valid syntax.
- [x] Cross-check every documented command and flag against the implementation spec and command recipes; expect no undocumented command claims.
