# 0020 — `scan-bridge` is the Home Assistant/MQTT-facing component, via MQTT discovery

- **Status:** Proposed
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** scan-bridge, metrics, config

## Context

The ESP32 panel firmware (`firmware/esp32-panel/cyd-scan-panel.yaml`) deliberately ships **no
`api:` block** — its own README states this is intentional: a publishable, secret-free binary
cannot embed a per-device Home Assistant encryption key, and the panel does not need HA discovery
to function. The 2026-08-13 vision
([`docs/roadmap/2026-08-13-scan-system-vision.md`](../roadmap/2026-08-13-scan-system-vision.md),
Epics D1/D2) nonetheless wants scan counts, chain status, versions, and connection state visible in
Home Assistant "for further automatic actions" — creating tension between "the panel has no HA
integration" and "smarthome-visible status should exist." `internal/metrics/metrics.go` today
exports exactly one Prometheus collector (`scan_bridge_build_info`); the richer metrics named there
as `TODO(phase 1.4)` are not built yet, and are a separate, unaffected concern from this decision.

## Decision

We will have **`scan-bridge`** (not the panel) **publish metrics and status** — scan counts, chain
status, component versions, connection state — **to the existing homelab MQTT broker**, using
**Home Assistant's MQTT discovery** convention (`homeassistant/<component>/<node_id>/<object_id>/
config` plus state topics), so `scan-bridge`-backed entities appear in Home Assistant automatically
without hand-written HA YAML packages. **No Prometheus integration is added for this surface** —
the existing `internal/metrics` Prometheus collector (Phase 1.4's separately tracked `TODO`s)
remains untouched; this decision is specifically about the HA-facing channel, not a replacement for
metrics scraping. This makes **`scan-bridge` the single HA-facing component**, resolving the D2
tension: the panel's no-`api:` stance stays exactly as shipped, because the panel never talks to HA
directly — it already reports to `scan-bridge` (via `GET /health`, `GET /profiles`, and the result
of each `POST /scan`), and `scan-bridge` is what relays whatever is HA-relevant onward via MQTT.

## Options considered

- **Option A — MQTT discovery published from `scan-bridge` (chosen):** resolves D2 without any
  firmware change; a single code path is authoritative for HA-visible state, matching the "bridge
  is the source of truth" pattern already used elsewhere (ADR
  [0018](0018-profile-storage-ordering-frequency.md)'s bridge-side sorting is the same shape of
  decision).
- **Option B — rely solely on Home Assistant's native Prometheus integration scraping the existing
  metrics endpoint, no MQTT:** requires the operator to maintain an HA-side Prometheus scrape
  config, and does not give discrete, auto-appearing HA entities (buttons/sensors) the way
  discovery does — rejected as the *sole* channel; the Prometheus endpoint is unaffected and can
  continue to exist alongside this decision for other consumers (e.g. Grafana).
- **Option C — give the panel its own `api:` block / direct HA integration:** rejected — this
  explicitly reopens the firmware's already-decided, deliberate no-`api:` design (secret-free
  public binary distribution) and moves the integration surface to the panel instead of
  `scan-bridge`, contradicting the vision's own framing that `scan-bridge` should own this.

## Consequences

- **Positive:** D2's tension is resolved without touching firmware at all; "connection status" now
  has an unambiguous meaning — it is `scan-bridge`'s own connectivity to its downstream dependencies
  (`sane-runtime`, chosen destinations) and to the panel, reported outward by `scan-bridge`, never
  the panel reporting to HA itself.
- **Negative / trade-offs:** adds an MQTT client dependency to `scan-bridge` for the first time —
  the same broker ADR [0019](0019-scanner-power-control-pluggable-interface.md)'s MQTT power-
  control backend uses, and the two clients should likely share one connection (implementation
  detail, not an architecture split). Broker host/port/credentials must be sourced from the
  existing homelab secret-management convention — **not decided by this ADR**, left as an explicit
  open item.
- **Neutral / follow-ups:** exact discovery topic/entity naming, and which metrics beyond scan-
  count are exposed (the vision left "weitere Metriken" unspecified), are implementation-level
  detail for the plan/spec that implements this ADR.

## References

- `docs/roadmap/2026-08-13-scan-system-vision.md`, Epics D1 and D2.
- `firmware/esp32-panel/README.md` (the no-`api:` design rationale).
- `components/scan-bridge/internal/metrics/metrics.go` (existing Prometheus collector, unaffected).
- ADR [0016](0016-destination-routing-pluggable-interface.md), ADR
  [0019](0019-scanner-power-control-pluggable-interface.md) (same pluggable-registry pattern /
  shared broker dependency).
- Homelab context: existing MQTT broker + Home Assistant (outside this repository).
