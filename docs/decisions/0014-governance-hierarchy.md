# 0014 — Governance hierarchy: ADR > AGENTS/guidelines > code

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** docs

## Context
ADRs, `AGENTS.md`/guideline docs, and code can all describe how the project works; conflicts need a
defined precedence and decisions must not drift silently.

## Decision
We will treat **accepted ADRs as the top authority** (**ADR > `AGENTS.md`/guidelines > code/README**).
A decision change requires a **new, superseding ADR** (old → `Superseded by NNNN`) plus the matching
doc updates in the same PR. No silent drift.

## Options considered
- **Option A — ADR-top hierarchy, supersede-don't-edit (chosen):** clear authority + decision history.
- **Option B — AGENTS.md as top authority:** loses the "why" and the supersession trail.

## Consequences
- Guidelines/`AGENTS.md` are the operative form of ADR decisions, not a replacement.
- Review agents flag ADR deviations as blocking.

## References
- `docs/decisions/README.md`; `.claude/rules/adr.md`; issue #10; PR #11.
