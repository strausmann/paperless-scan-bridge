# 0005 — Trigger-source-agnostic `POST /scan` is the canonical interface

- **Status:** Accepted   <!-- accepted 2026-08-06 via #19 (Phase 1.2 reconciliation) -->
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** api, scan-bridge

## Context
Many trigger sources are desired (HA/Zigbee blueprint, n8n, web UI, curl, a future ESP32 panel); the
core should not special-case any of them.

## Decision
We will make **`POST /scan {profile}` the single canonical trigger**. Every trigger source ultimately
calls this endpoint — "if you can speak HTTP and bearer auth, you can trigger."

## Options considered
- **Option A — one canonical HTTP trigger (chosen):** uniform, extensible, no per-source code.
- **Option B — per-source integrations in the core:** couples the daemon to each trigger; rejected.

## Consequences
- New trigger devices (e.g. ESP32 panel #9) are just HTTP clients; no core change.
- Hardware-button paths (scanbd, #7) are *one possible* client, not a special case.

## References
- `ARCHITECTURE.md` ("canonical interface"); `components/scan-bridge/internal/api/routes.go`;
  issue #9.
