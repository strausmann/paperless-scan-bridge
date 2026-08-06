# 0006 — Auth model: bearer token (SHA-256 hash) or IP allowlist

- **Status:** Accepted   <!-- accepted 2026-08-06 via #19 (Phase 1.2 reconciliation) -->
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** scan-bridge, config

## Context
The API can trigger scans and must be authenticated, while staying simple for a home-lab deployment
behind a proxy.

## Decision
We will support two auth modes, default **`token`**: compare the **SHA-256 hex digest** of a bearer
token against a configured `token_hash` (plaintext supplied via env, never retained); or
**`ip_allowlist`** (CIDR match). Mode is set by `auth.mode`.

## Options considered
- **Option A — token (hashed) or ip_allowlist (chosen):** simple, no plaintext secret at rest,
  proxy-friendly.
- **Option B — full OAuth/session:** overkill for the use case.

## Consequences
- Tokens are stored only as hashes; the plaintext lives in the client/env.
- **Enforcement middleware is pending** (Phase 1.4) — the model/schema is decided and implemented in
  config; wiring it into the API handlers is the remaining step.

## References
- `components/scan-bridge/internal/config/config.go` (`AuthMode`, `TokenHash`, allowlist);
  `CONTAINER_SUITE.md` §4.5; issue #9 (auth).
