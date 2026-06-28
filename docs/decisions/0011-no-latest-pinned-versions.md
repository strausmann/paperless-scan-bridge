# 0011 — No `latest` in production; pin versions; Renovate-driven updates

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** docker, deploy, deps

## Context
Reproducible deployments require deterministic image versions; updates should be reviewed, not
implicit.

## Decision
We will **never reference `latest` in production compose**; pin images by **SemVer tag** (digest-pin as
the target), version the three images independently (strict SemVer 2.0.0), and let **Renovate** open
update PRs for human review.

## Options considered
- **Option A — pinned versions + Renovate (chosen):** reproducible, reviewed updates.
- **Option B — `latest`/auto-update:** non-reproducible, surprise breakage; rejected.

## Consequences
- **Open follow-up:** digest-pinning is the target but not yet enforced (`Dockerfile` TODO "pin base
  by digest once Renovate is wired"); a `latest` tag may still be *published*, the rule governs
  *consumption in production*.

## References
- `CONTAINER_SUITE.md` §2.5/§3; `AGENTS.md`; `CLAUDE.md`; `.github/copilot-instructions.md`; PR
  template.
