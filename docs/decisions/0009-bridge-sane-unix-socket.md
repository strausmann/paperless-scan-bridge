# 0009 — scan-bridge ↔ sane-runtime communicate over HTTP on a Unix socket

- **Status:** Accepted   <!-- accepted 2026-08-06 via #19 (Phase 1.2 reconciliation) -->
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** scan-bridge, sane-runtime, dispatch

## Context
The two containers must exchange scan jobs without opening network ports or risking accidental
exposure.

## Decision
We will have `scan-bridge` dispatch to `sane-runtime` via **HTTP over a shared Unix-domain socket**
(named volume), **not TCP**.

## Options considered
- **Option A — HTTP over a Unix socket (chosen):** no ports, no accidental exposure, simple.
- **Option B — TCP between containers:** opens a port, larger surface; rejected.

## Consequences
- A shared named volume carries the socket.
- **Open detail:** the socket *path* differs between sources (`config.go` `/run/sane-runtime/sane.sock`
  vs `CONTAINER_SUITE.md` §7.1 `/var/run/scan-runtime/api.sock`) — the transport is firm, the path
  should be finalized.

## References
- `CONTAINER_SUITE.md` §7.1; `ARCHITECTURE.md`; `components/scan-bridge/internal/config/config.go`
  (`Paths.SaneSocket`).
