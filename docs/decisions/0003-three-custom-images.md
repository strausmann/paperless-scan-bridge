# 0003 — Exactly three custom images (scan-bridge / sane-runtime / scan-processor)

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** docker, scan-bridge, sane-runtime, scan-processor

## Context
Separation of concerns, independent update cadence, and least-privilege drive how we split the stack;
upstream tools (Paperless-ngx, etc.) are adopted, not forked.

## Decision
We will build and publish **exactly three single-purpose images** on GHCR —
`scan-bridge` (API + dispatch), `sane-runtime` (SANE/scanbd/USB), `scan-processor` (PDF/ingestion).
A mega-image is rejected; upstream images are adopted via compose/config, never forked.

## Options considered
- **Option A — three single-purpose images (chosen):** independent updates, isolated privilege,
  scalable processing.
- **Option B — one combined image:** couples update cadence and privilege; rejected.

## Consequences
- Inter-container IPC is required (see ADR 0009).
- A SANE security patch redeploys only `sane-runtime`.

## References
- `CONTAINER_SUITE.md` §1 (table + "Why three containers"); `ARCHITECTURE.md`; `components/`;
  commit-scope set in `commitlint.config.cjs`.
