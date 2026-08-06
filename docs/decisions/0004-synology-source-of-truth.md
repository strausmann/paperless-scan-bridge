# 0004 — Synology NAS is the single source of truth for documents

- **Status:** Proposed
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

## References
- `ARCHITECTURE.md`; `CONCEPT.md` §11.1; `CLAUDE.md`; `README.md`.
