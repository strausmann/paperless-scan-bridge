# 0019 — Scanner power control via a pluggable `PowerControl` interface + registry

- **Status:** Proposed
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** scan-bridge, config, deploy

## Context

The 2026-08-13 vision
([`docs/roadmap/2026-08-13-scan-system-vision.md`](../roadmap/2026-08-13-scan-system-vision.md),
Epics C1/C2) wants the scanner powered on for a job via a smart plug and powered off after a
configurable idle period. No existing code or ADR touches power control today — this is a new
integration surface. The vision names both WiFi-Tasmota and Zigbee2MQTT-bridged plugs as candidate
device families (e.g. Nous A1/A5), and explicitly frames this as something **`scan-bridge`
controls, not the panel** — consistent with the panel firmware's deliberate no-`api:`, secret-free
public-binary design (see ADR [0020](0020-mqtt-home-assistant-integration.md)).

## Decision

We will add a **`PowerControl` Go interface** and a **registry** (name → constructor) to
`scan-bridge`, mirroring the `Destination` pattern in ADR
[0016](0016-destination-routing-pluggable-interface.md). First backends: **MQTT** (covers both
Tasmota-over-MQTT and Zigbee2MQTT-bridged devices via the existing homelab broker) and
**Tasmota-HTTP-direct** (calling the plug's own HTTP API, no broker hop required). A **webhook-
style backend is deferred** as a later addition once a concrete need appears. An **idle-off timer**
(configurable duration) lives in `scan-bridge`: the daemon issues power-on when a scan is triggered
("turn-on-for-job"), and starts the idle timer after each completed scan, powering the scanner off
once the timer elapses without a new trigger.

## Options considered

- **Option A — pluggable `PowerControl` interface + registry, MQTT and Tasmota-HTTP first
  (chosen):** extensible to future device families without touching the scan dispatch core;
  Tasmota-HTTP-direct needs neither Zigbee2MQTT nor a broker, covering the vision's WiFi-Tasmota-
  only example directly.
- **Option B — Home-Assistant-mediated only (bridge calls an HA service):** would reuse existing HA
  automations, but makes a home-lab feature hard-depend on HA being reachable and configured, and
  is not ruled out as a *future additional* backend — just not the only path decided here.
- **Option C — out-of-band automation entirely in Home Assistant, triggered by the panel's own
  webhook call:** rejected — moves ownership out of `scan-bridge`, contradicts the vision's
  explicit "controlled by scan-bridge, not the panel" framing, and would split the idle-timer state
  across two systems (HA automation and `scan-bridge`'s own status reporting) instead of one.

## Consequences

- **Positive:** new device families are additive (a new package implementing `PowerControl` plus a
  registration call); the Tasmota-HTTP-direct backend works standalone without any broker
  dependency.
- **Negative / trade-offs:** the MQTT backend adds a broker client dependency to `scan-bridge` for
  the first time — it connects to the same existing homelab broker ADR 0020 uses for HA/MQTT
  status, and the two clients should likely share one connection (an implementation note, not an
  architecture split). Whether `POST /scan` blocks on scanner warm-up (readiness polling before the
  SANE call) or a separate "wake" step is required is a distinct implementation question this ADR
  does not resolve.
- **Neutral / follow-ups:** specific smart-plug models (the vision names Nous A1/A5) are a
  hardware-compatibility-matrix concern (`HARDWARE_COMPATIBILITY.md`-style), not an architecture
  decision. The idle-timeout default value, and whether it is a single global setting or
  per-profile, are implementation details for the plan/spec that implements this ADR.

## References

- `docs/roadmap/2026-08-13-scan-system-vision.md`, Epics C1 and C2.
- ADR [0016](0016-destination-routing-pluggable-interface.md) (same pluggable interface + registry
  pattern, applied here to power control instead of destinations).
- ADR [0020](0020-mqtt-home-assistant-integration.md) (shared MQTT broker dependency).
- Homelab context: Zigbee2MQTT + MQTT broker + Home Assistant already running in the homelab
  (outside this repository).
