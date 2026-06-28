# 0010 — Scan profiles are declarative YAML, validated at startup

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** profiles, scan-bridge

## Context
Scan behaviour (source, resolution, mode, tags) must be predictable, reviewable, and validated.

## Decision
We will define scan behaviour as **named profiles in a YAML file**, validated **strictly at daemon
startup** (unknown fields rejected; the daemon refuses to start with zero profiles).

## Options considered
- **Option A — declarative YAML, strict startup validation (chosen):** simple, reviewable, fail-fast.
- **Option B — database-backed profiles from day one:** premature; YAML is sufficient now.

## Consequences
- Profiles are config, version-controllable.
- **Open follow-up:** issue #9 contemplates moving to a DB + new fields
  (`display_order`/`display_enabled`/`color`/`label`) once a management UI lands — to be decided then
  (do not extend this ADR pre-emptively).

## References
- `components/scan-bridge/internal/profiles/profiles.go` (strict `KnownFields`, refuses empty),
  `defaults.yaml`; `ARCHITECTURE.md`.
