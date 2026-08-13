# 0004 — Synology NAS is the single source of truth for documents

- **Status:** Superseded by 0016
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** deploy

## Context
Documents must have one canonical, backed-up home independent of the scanning host's lifecycle.

## Decision
We will treat the **Synology NAS as the canonical store** for all documents/archives/backups,
regardless of the chosen storage topology. The scanning host is an **ingestion node, not a storage
node**.

## Options considered
- **Option A — Synology as SoR (chosen):** durable, backed-up, host-independent.
- **Option B — store on the Pi/host:** couples data to a disposable host; rejected.

## Consequences
- The host can be re-flashed without data loss.
- Note: the host hardware is broadening from "Raspberry Pi" to "any always-on Linux box" (AGENTS.md,
  issue #9) — this does not change the storage-authority decision.
- **Note (2026-08-13):** ADR [0016](0016-destination-routing-pluggable-interface.md) introduces
  per-profile destination routing, of which Synology's NFS/SMB share is one destination among
  several (Paperless-ngx, fileee, and a generic HTTP-POST destination can now also be targeted
  directly). The operator has confirmed Synology archival becomes **purely per-profile** rather
  than staying mandatory for every profile — this ADR is superseded by 0016 accordingly; see that
  ADR's "Interaction with ADR 0004" section for the confirmed resolution.

## References
- `ARCHITECTURE.md`; `CONCEPT.md` §11.1; `CLAUDE.md`; `README.md`.
- ADR [0016](0016-destination-routing-pluggable-interface.md) (2026-08-13 interaction note above).
