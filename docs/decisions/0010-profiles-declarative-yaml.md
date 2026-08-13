# 0010 — Scan profiles are declarative YAML, validated at startup

- **Status:** Accepted   <!-- accepted 2026-08-06; rationale: docs/superpowers/plans/2026-08-06-phase-1.2-adr-reconciliation.md (D-6), tracking issue #19 -->
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
- **Update (2026-08-13):** ADR [0018](0018-profile-storage-ordering-frequency.md) proposes the
  resolution to this follow-up — display/ordering/usage-frequency state moves to SQLite, while the
  scan-parameter fields this ADR governs stay YAML-authored exactly as decided here. 0018 fulfills
  this follow-up; it does not supersede this ADR.

## References
- `components/scan-bridge/internal/profiles/profiles.go` (strict `KnownFields`, refuses empty),
  `defaults.yaml`; `ARCHITECTURE.md`.
- ADR [0018](0018-profile-storage-ordering-frequency.md) (2026-08-13 follow-up resolution).
