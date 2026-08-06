# 0015 — Per-profile token is an optional authorization layer over canonical `POST /scan`

- **Status:** Accepted
- **Date:** 2026-08-06
- **Deciders:** strausmann
- **Tags:** api, scan-bridge, config

## Context

ADR [0005](0005-trigger-agnostic-scan-endpoint.md) makes `POST /scan {profile}`
the single canonical trigger, and ADR [0006](0006-auth-model.md) sets the daemon
auth to a hashed bearer token (or IP allowlist). The 2026-04-30 Phase 1.2 plan
instead proposed a per-profile `POST /webhooks/<token>` trigger whose secret
token lived in the URL path (spec open question O-3, stored HMAC-SHA256). During
reconciliation (issue #19) we had to decide the fate of that per-profile token
now that `/scan` + bearer is canonical: drop it, or keep it — and if kept, in
what role.

## Decision

We will **keep the per-profile token as an *optional, additional* authorization
check on `POST /scan`**, not as the primary auth and not in the URL. The daemon
**bearer token (ADR 0006) is always the first gate**; when a profile carries a
token, a caller targeting that profile must *also* present the profile token
(via header/body, never the URL path). Profile tokens are stored **HMAC-SHA256**
under a daemon-wide server secret, never in plaintext; the plaintext is returned
exactly once on create/rotate. A per-profile **webhook-style trigger remains a
deferred future option** and, when added, will be an *additional* client of the
same scan action — never a replacement for `/scan`.

## Options considered

- **Option A — keep per-profile token as optional authz layer (chosen):**
  preserves per-profile isolation/revocation for callers that want it, keeps the
  O-3 HMAC storage work, and stays fully compatible with ADR 0005/0006 because
  `/scan` + bearer remains canonical.
- **Option B — drop per-profile tokens entirely:** simplest, but loses
  per-profile authorization granularity and throws away the O-3 model.
- **Option C — make per-profile webhook tokens the primary trigger:** rejected —
  contradicts ADR 0005 and leaks secrets in the URL path.

## Consequences

- **Positive:** callers can be scoped to a single profile without new endpoints;
  revoking one profile token does not rotate the shared bearer; secret never
  appears in a URL.
- **Negative / trade-offs:** two auth concepts coexist (bearer + optional profile
  token); the middleware must check both and tests must cover the layered path.
- **Neutral / follow-ups:** the deferred webhook trigger, when built, reuses this
  token model; keep `internal/config` auth fields and the profile store's
  `RotateToken` in sync with this ADR.

## References

- ADRs [0005](0005-trigger-agnostic-scan-endpoint.md),
  [0006](0006-auth-model.md); reconciliation
  `docs/superpowers/plans/2026-08-06-phase-1.2-adr-reconciliation.md`; issue #19;
  spec open question O-3 / plan correction Δ-7.
