# 0016 — Destination routing via a pluggable `Destination` interface + registry

- **Status:** Proposed
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** scan-processor, profiles, config

## Context

The current profile schema (`components/scan-bridge/internal/profiles/profiles.go`) has no
destination concept at all — `MetadataTemplate` only carries `PaperlessTags`/
`PaperlessCorrespondent`, and every scan implicitly ends up at Paperless-ngx via the Synology NFS
consume directory (ADR [0004](0004-synology-source-of-truth.md)). The 2026-08-13 vision (see
[`docs/roadmap/2026-08-13-scan-system-vision.md`](../roadmap/2026-08-13-scan-system-vision.md),
Epic A1) wants a profile to be able to target Paperless-ngx, fileee, a plain NFS/SMB share, or a
simple own HTTP API — and to choose, per profile, whether the scan goes to intermediate storage
first or straight to an API. This needs an extension point that does not require touching the
dispatch core every time a new destination shows up.

## Decision

We will add a **`Destination` Go interface** and a **registry** (name → constructor) to
`scan-bridge`/`scan-processor`. Built-in destination modules, each a self-contained Go package
implementing the interface and registering itself: **NFS**, **SMB**, **Paperless-ngx** (API
upload), **fileee** (API upload), and a **generic HTTP-POST destination** for simple own APIs.
Adding a new destination is a new package implementing `Destination` plus a registration call —
the dispatch core is not modified. Each profile's config chooses one or more target destinations
and, independently, whether the scan output is written to intermediate storage (NFS/SMB) first and
then picked up/pushed to an API destination, or goes directly to an API destination — the latter
exists to avoid unnecessary SD-card wear on the Pi and to allow OCR post-processing to run before
upload. **Multiple destinations per profile are allowed** (fan-out to more than one target from a
single scan).

**Interaction with ADR [0004](0004-synology-source-of-truth.md):** under this model, Synology
becomes **one (common) NFS/SMB destination among several**, not a mandatory single sink baked into
the architecture. This ADR does **not** decide whether Synology archival stays mandatory for every
profile regardless of chosen destinations, or becomes purely a per-profile opt-in like any other
destination — **that needs explicit operator confirmation** (see References and the pull request
description). ADR 0004's status is left as-is pending that confirmation; it is not superseded here.

## Options considered

- **Option A — pluggable `Destination` interface + registry (chosen):** extensible without core
  changes, matches "adding a destination = new package + registration," supports fan-out and the
  storage-first-vs-direct-to-API choice per profile.
- **Option B — hardcode Paperless and fileee as two special-cased code paths:** simplest to write
  first, but does not scale to "and a simple own API" from the vision, and re-couples the core to
  destination semantics every time a new target is added — rejected.
- **Option C — single mandatory destination, always via Synology first:** simplest mental model,
  keeps ADR 0004 untouched, but does not meet the vision's explicit direct-to-API requirement
  (SD-wear avoidance, pre-upload OCR) — rejected.

## Consequences

- **Positive:** new destinations (a future second self-hosted document system, for example) are
  additive; the profile schema gains a clean, testable seam (`Destination.Send(ctx, doc) error`-
  shaped interface) instead of destination-specific branching in the dispatch core.
- **Negative / trade-offs:** the profile schema grows a `destinations` list plus a per-entry
  "storage-first vs. direct" flag — more config surface to validate strictly at startup, in the
  spirit of ADR [0010](0010-profiles-declarative-yaml.md). Each built-in destination module needs
  its own auth/config and its own test coverage (Happy-Path + Error-Path + Network-Error per the
  repo's mutation-function testing convention).
- **Neutral / follow-ups:** the exact `Destination` interface signature, the `profiles.go` schema
  extension, and the fileee client mechanism (existing `go-fileee` library vs. a new client) are
  implementation details for the plan that implements this ADR, not decided here. The fileee
  **account/auth** model (which fileee account an upload targets, how that is referenced from a
  profile) is an explicit open sub-question, tracked in the roadmap triage doc, not resolved by
  this ADR.

## References

- `docs/roadmap/2026-08-13-scan-system-vision.md`, Epic A1 (open questions 1–4).
- ADR [0004](0004-synology-source-of-truth.md) (Synology as source of truth — interaction above).
- ADR [0010](0010-profiles-declarative-yaml.md) (strict startup validation convention).
- `components/scan-bridge/internal/profiles/profiles.go` (`MetadataTemplate`, current
  Paperless-only shape).
- Homelab context: `go-fileee` (Go client library for the internal fileee API),
  `fileee-mcp-server`.
