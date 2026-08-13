# 0018 — Profile display metadata, bridge-side ordering, and usage-frequency tracking

- **Status:** Proposed
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** profiles, api, firmware

## Context

ADR [0010](0010-profiles-declarative-yaml.md) declared profiles as declarative YAML, strictly
validated at daemon startup, and explicitly **deferred** a follow-up: "issue #9 contemplates
moving to a DB + new fields (`display_order`/`display_enabled`/`color`/`label`) once a management
UI lands — to be decided then (do not extend this ADR pre-emptively)." The ESP32 panel (issue #9,
shipped, `firmware/esp32-panel/cyd-scan-panel.yaml`) already fetches `GET /profiles` today and
renders a fixed 6-button grid with no ordering. The 2026-08-13 vision
([`docs/roadmap/2026-08-13-scan-system-vision.md`](../roadmap/2026-08-13-scan-system-vision.md),
Epic B3) asks for configurable ordering — alphabetical, manual, usage-frequency, and a mixed mode —
plus display fields, which is exactly the condition ADR 0010 said would trigger this decision.
ADR [0005](0005-trigger-agnostic-scan-endpoint.md)'s design intent (issue #9's original API
planning table) already states `GET /profiles` should return an already-sorted list "so the
firmware stays dumb."

## Decision

We will extend profiles with **display metadata** — order, enabled, a short display label, and a
color — alongside the existing YAML-authored scan parameters (source, resolution, mode, format,
etc., which stay exactly as ADR 0010 defined them). `GET /profiles` will support **four ordering
modes**: **alphabetical**, **manual** (an explicit per-profile order value), **usage-frequency**
(descending scan count), and **mixed** — an operator-configurable number of manually pinned slots
first, followed by the remainder sorted by frequency. **Ordering is computed bridge-side**: `GET
/profiles` always returns an already-sorted list; the firmware and any future client never sort
client-side, extending ADR 0005's dumb-firmware principle from "which endpoint to call" to "what
order to render." Usage-frequency tracking requires per-profile scan counts to survive restarts,
which requires the **SQLite persistence layer ADR 0010 explicitly deferred** — this ADR is the
"management UI lands" trigger event ADR 0010 anticipated. Only **display/ordering/count state**
moves to SQLite; the scan-parameter fields a profile is defined by remain YAML-authored exactly as
ADR 0010 decided — this ADR fulfills that ADR's deferred follow-up, it does not supersede it.

## Options considered

- **Option A — bridge-side sorting + SQLite-backed display/count state (chosen):** a single sort
  implementation is reused by every client (panel today, a future web UI or `n8n` reader), matches
  the already-documented dumb-firmware intent, and reuses the DB layer ADR 0010 already
  anticipated.
- **Option B — panel-side sorting (firmware fetches the raw list and sorts itself):** keeps the
  bridge simpler, but contradicts the already-documented design principle and forces every future
  client (web UI, `n8n`, anything else) to reimplement the same sort logic — rejected.
- **Option C — keep order/count in the YAML config file, no DB:** avoids adding a stateful
  dependency, but usage-frequency counts mutate on every scan and are not a fit for a
  version-controlled config file — this is exactly why ADR 0010 deferred to a DB in the first
  place — rejected.

## Consequences

- **Positive:** a single, testable sort implementation serves every current and future client;
  usage-frequency tracking also unlocks other vision items that need scan counts (metrics/HA
  exposure, see ADR [0020](0020-mqtt-home-assistant-integration.md)).
- **Negative / trade-offs:** introduces `scan-bridge`'s first stateful DB dependency, adding a
  migration and backup surface that did not exist before; the mixed-mode "how many pinned slots"
  needs a config knob with a sane default, and must be validated at startup in the spirit of ADR
  0010's strict validation.
- **Neutral / follow-ups:** the exact SQLite schema, the migration path from pure-YAML profiles to
  YAML-plus-SQLite-display-state, and the specific mixed-mode default slot count are implementation
  details for the plan that implements this ADR. "Manual" ordering still needs the Phase 1.4
  profile-management UI (issue #9 phase B, drag-and-drop) to be *set* conveniently — this ADR only
  decides that manual is a legitimate ordering *mode* returned by the API, not that the UI to set
  it exists yet.

## References

- `docs/roadmap/2026-08-13-scan-system-vision.md`, Epic B3.
- ADR [0010](0010-profiles-declarative-yaml.md) (deferred follow-up this ADR fulfills).
- ADR [0005](0005-trigger-agnostic-scan-endpoint.md) (dumb-firmware principle).
- `docs/superpowers/plans/2026-04-30-phase-1.2-webhook-architecture.md`, Task 3 (already-sketched
  `scan_count_total` column).
- `firmware/esp32-panel/cyd-scan-panel.yaml` and its `README.md` (current fixed 6-button grid,
  unordered).
- Issue #9 (panel design, phase B — drag-and-drop management UI).
