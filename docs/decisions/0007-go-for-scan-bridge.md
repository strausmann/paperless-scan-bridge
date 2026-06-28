# 0007 — scan-bridge is written in Go (static binary, distroless)

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** scan-bridge, scan-processor, docker

## Context
The daemon needs fast startup, a small footprint, easy ARM64 cross-compilation, and a minimal attack
surface in the container.

## Decision
We will implement `scan-bridge` (and `scan-processor`) in **Go**, shipping a **static binary on a
distroless base**.

## Options considered
- **Option A — Go + distroless (chosen):** tiny static image, fast start, easy cross-compile.
- **Option B — Python/Node service:** larger image, runtime deps; rejected for the daemon.

## Consequences
- Small, distroless `nonroot` images (size goals enforced).
- Go toolchain is the build dependency (in CI/containers, not on the host).

## References
- `ARCHITECTURE.md`; `CLAUDE.md` (tech table); `CONTAINER_SUITE.md` (distroless, "no Alpine");
  `components/scan-bridge/go.mod`, `Dockerfile` (`distroless/static-debian12:nonroot`).
