# 0013 — Container hardening baseline: non-root, read-only rootfs, drop all caps

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** docker, sane-runtime, scan-bridge, scan-processor

## Context
All custom containers should run with least privilege by default to limit blast radius.

## Decision
We will run all three custom containers as **non-root with fixed UIDs**, with a **read-only root
filesystem** (explicit writable volumes/tmpfs only) and **`cap_drop: [ALL]`** (adding back only what a
container provably needs).

## Options considered
- **Option A — non-root + read-only + drop-all-caps (chosen):** strong default hardening.
- **Option B — default Docker (root, writable, full caps):** unnecessary privilege; rejected.

## Consequences
- Writable paths must be declared explicitly (volumes/tmpfs).
- `sane-runtime` adds back only the capability it needs (see ADR 0008).

## References
- `CONTAINER_SUITE.md` §2.6–§2.8; `components/scan-bridge/Dockerfile` (`USER nonroot`); `AGENTS.md`.
